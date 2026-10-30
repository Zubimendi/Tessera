package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Store struct {
	DB    *pgxpool.Pool
	Redis *redis.Client
}

func New(ctx context.Context, dbURL, redisAddr string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := client.Ping(ctx).Err(); err != nil {
		pool.Close()
		client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &Store{
		DB:    pool,
		Redis: client,
	}, nil
}

func (s *Store) Close() {
	if s == nil {
		return
	}
	if s.DB != nil {
		s.DB.Close()
	}
	if s.Redis != nil {
		s.Redis.Close()
	}
}
