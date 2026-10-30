package geo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultGeoKey = "geo:avail:default"

type RedisStore struct {
	client *redis.Client
	key    string
}

func NewRedisStore(client *redis.Client, region string) *RedisStore {
	key := defaultGeoKey
	if region != "" {
		key = "geo:avail:" + region
	}
	return &RedisStore{client: client, key: key}
}

func (s *RedisStore) AddAvailable(ctx context.Context, driverID string, point Point) error {
	if err := ValidatePoint(point); err != nil {
		return err
	}
	if driverID == "" {
		return fmt.Errorf("driver id is required")
	}
	return s.client.GeoAdd(ctx, s.key, &redis.GeoLocation{
		Longitude: point.Lng,
		Latitude:  point.Lat,
		Name:      driverID,
	}).Err()
}

func (s *RedisStore) RemoveAvailable(ctx context.Context, driverID string) (bool, error) {
	removed, err := s.client.ZRem(ctx, s.key, driverID).Result()
	if err != nil {
		return false, err
	}
	return removed == 1, nil
}

func (s *RedisStore) SearchNearby(ctx context.Context, center Point, radiusKm float64, limit int) ([]Candidate, error) {
	if err := ValidatePoint(center); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}

	results, err := s.client.GeoSearchLocation(ctx, s.key, &redis.GeoSearchLocationQuery{
		GeoSearchQuery: redis.GeoSearchQuery{
			Longitude:  center.Lng,
			Latitude:   center.Lat,
			Radius:     radiusKm,
			RadiusUnit: "km",
			Sort:       "ASC",
			Count:      limit,
		},
		WithCoord: true,
		WithDist:  true,
	}).Result()
	if err != nil {
		return nil, err
	}

	candidates := make([]Candidate, 0, len(results))
	for _, item := range results {
		loc := Point{Lat: item.Latitude, Lng: item.Longitude}
		if err := ValidatePoint(loc); err != nil {
			continue
		}
		candidates = append(candidates, Candidate{
			DriverID: item.Name,
			Point:    loc,
			Distance: item.Dist,
		})
	}
	return candidates, nil
}

func (s *RedisStore) SetDriverStatus(ctx context.Context, driverID, status, offerID string) error {
	if driverID == "" {
		return fmt.Errorf("driver id is required")
	}
	key := "driver:" + driverID
	if offerID != "" {
		return s.client.HSet(ctx, key, "status", status, "offer_id", offerID).Err()
	}
	return s.client.HSet(ctx, key, "status", status).Err()
}

func (s *RedisStore) SetDriverPing(ctx context.Context, driverID string, point Point) error {
	if err := ValidatePoint(point); err != nil {
		return err
	}
	key := "driver:" + driverID
	return s.client.HSet(ctx, key,
		"lat", strconv.FormatFloat(point.Lat, 'f', -1, 64),
		"lng", strconv.FormatFloat(point.Lng, 'f', -1, 64),
		"last_ping_at", time.Now().UnixMilli(),
		"status", "AVAILABLE",
	).Err()
}

func (s *RedisStore) ClaimDriver(ctx context.Context, driverID, offerID string) (bool, error) {
	if driverID == "" {
		return false, fmt.Errorf("driver id is required")
	}
	if offerID == "" {
		return false, fmt.Errorf("offer id is required")
	}

	script := redis.NewScript(`
		local key = KEYS[1]
		local driver = ARGV[1]
		local offerId = ARGV[2]
		if redis.call('ZREM', key, driver) == 1 then
			redis.call('HSET', 'driver:' .. driver, 'status', 'OFFERED', 'offer_id', offerId)
			return 1
		end
		return 0
	`)

	result, err := script.Run(ctx, s.client, []string{s.key}, driverID, offerID).Result()
	if err != nil {
		return false, err
	}
	value, err := strconv.Atoi(fmt.Sprint(result))
	if err != nil {
		return false, err
	}
	return value == 1, nil
}

func (s *RedisStore) ReleaseDriver(ctx context.Context, driverID, offerID string, point Point) (bool, error) {
	if err := ValidatePoint(point); err != nil {
		return false, err
	}

	script := redis.NewScript(`
		local key = KEYS[1]
		local driver = ARGV[1]
		local offerId = ARGV[2]
		local lat = ARGV[3]
		local lng = ARGV[4]
		local currentStatus = redis.call('HGET', 'driver:' .. driver, 'status')
		local currentOffer = redis.call('HGET', 'driver:' .. driver, 'offer_id')
		if currentStatus ~= 'OFFERED' or currentOffer ~= offerId then
			return 0
		end
		redis.call('HSET', 'driver:' .. driver, 'status', 'AVAILABLE', 'offer_id', '', 'lat', lat, 'lng', lng)
		redis.call('GEOADD', key, lng, lat, driver)
		return 1
	`)

	result, err := script.Run(ctx, s.client, []string{s.key}, driverID, offerID, strconv.FormatFloat(point.Lat, 'f', -1, 64), strconv.FormatFloat(point.Lng, 'f', -1, 64)).Result()
	if err != nil {
		return false, err
	}
	value, err := strconv.Atoi(fmt.Sprint(result))
	if err != nil {
		return false, err
	}
	return value == 1, nil
}

func (s *RedisStore) SweepStale(ctx context.Context, maxAge time.Duration) error {
	keys, _, err := s.client.Scan(ctx, 0, "driver:*", 100).Result()
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-maxAge).UnixMilli()
	for _, key := range keys {
		v, err := s.client.HGet(ctx, key, "last_ping_at").Result()
		if err != nil || v == "" {
			continue
		}
		value, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		if value < cutoff {
			driverID := strings.TrimPrefix(key, "driver:")
			if _, err := s.RemoveAvailable(ctx, driverID); err != nil {
				return err
			}
			_, err = s.client.HDel(ctx, key, "status", "offer_id").Result()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
