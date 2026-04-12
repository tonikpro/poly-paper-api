package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tonikpro/poly-paper-api/internal/models"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateUser(ctx context.Context, email, passwordHash, ethAddress string, ethPrivateKeyEncrypted []byte) (*models.User, error) {
	user := &models.User{}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, eth_address, eth_private_key_encrypted)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, email, eth_address, created_at`,
		email, passwordHash, ethAddress, ethPrivateKeyEncrypted,
	).Scan(&user.ID, &user.Email, &user.EthAddress, &user.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	user := &models.User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, eth_address, eth_private_key_encrypted, created_at
		 FROM users WHERE email = $1`,
		email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.EthAddress, &user.EthPrivateKeyEncrypted, &user.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return user, nil
}

func (r *Repository) GetUserByEthAddress(ctx context.Context, ethAddress string) (*models.User, error) {
	user := &models.User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, eth_address, eth_private_key_encrypted, created_at
		 FROM users WHERE LOWER(eth_address) = LOWER($1)`,
		ethAddress,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.EthAddress, &user.EthPrivateKeyEncrypted, &user.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get user by eth address: %w", err)
	}
	return user, nil
}

func (r *Repository) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	user := &models.User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, eth_address, eth_private_key_encrypted, created_at
		 FROM users WHERE id = $1`,
		id,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.EthAddress, &user.EthPrivateKeyEncrypted, &user.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return user, nil
}

func (r *Repository) CreateCollateralWallet(ctx context.Context, userID string, initialBalance string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO wallets (user_id, asset_type, token_id, balance, allowance)
		 VALUES ($1, 'COLLATERAL', '', $2, $2)`,
		userID, initialBalance,
	)
	return err
}

// API Key operations

func (r *Repository) CreateAPIKey(ctx context.Context, userID, id, apiSecret, passphrase string) (*models.APIKey, error) {
	key := &models.APIKey{}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO api_keys (id, user_id, api_secret, passphrase)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, api_secret, passphrase, created_at`,
		id, userID, apiSecret, passphrase,
	).Scan(&key.ID, &key.APISecret, &key.Passphrase, &key.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	key.UserID = userID
	return key, nil
}

func (r *Repository) GetAPIKeysByUserID(ctx context.Context, userID string) ([]models.APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, api_secret, passphrase, created_at
		 FROM api_keys
		 WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get api keys: %w", err)
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var key models.APIKey
		if err := rows.Scan(&key.ID, &key.UserID, &key.APISecret, &key.Passphrase, &key.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (r *Repository) GetAPIKeyByKey(ctx context.Context, apiKey string) (*models.APIKey, error) {
	key := &models.APIKey{}
	err := r.pool.QueryRow(ctx,
		`SELECT ak.id, ak.user_id, ak.api_secret, ak.passphrase, ak.created_at
		 FROM api_keys ak WHERE ak.id = $1`,
		apiKey,
	).Scan(&key.ID, &key.UserID, &key.APISecret, &key.Passphrase, &key.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return key, nil
}

func (r *Repository) GetAPIKeyByUserID(ctx context.Context, userID string) (*models.APIKey, error) {
	key := &models.APIKey{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, api_secret, passphrase, created_at
		 FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1`,
		userID,
	).Scan(&key.ID, &key.UserID, &key.APISecret, &key.Passphrase, &key.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get api key by user: %w", err)
	}
	return key, nil
}

func (r *Repository) DeleteAPIKey(ctx context.Context, apiKey string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, apiKey)
	return err
}

func (r *Repository) DeleteAPIKeyForUser(ctx context.Context, userID, apiKey string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM api_keys WHERE user_id = $1 AND id = $2`, userID, apiKey)
	return err
}

// Nonce tracking for L1 replay protection

func (r *Repository) IsNonceUsed(ctx context.Context, ethAddress string, nonce, timestamp int64) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM used_nonces
		 WHERE LOWER(eth_address) = LOWER($1) AND nonce = $2 AND timestamp = $3
		 AND used_at > now() - interval '600 seconds')`,
		ethAddress, nonce, timestamp,
	).Scan(&exists)
	return exists, err
}

func (r *Repository) RecordNonce(ctx context.Context, ethAddress string, nonce, timestamp int64) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO used_nonces (eth_address, nonce, timestamp)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (eth_address, nonce) DO UPDATE SET timestamp = EXCLUDED.timestamp, used_at = now()`,
		ethAddress, nonce, timestamp,
	)
	return err
}
