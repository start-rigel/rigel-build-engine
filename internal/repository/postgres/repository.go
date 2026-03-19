package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
)

type Repository struct {
	db *sql.DB
}

func New(ctx context.Context, dsn string) (*Repository, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *Repository) ListProducts(ctx context.Context, platforms []model.SourcePlatform, limit int) ([]model.Product, error) {
	if limit <= 0 {
		limit = 100
	}
	platformValues := make([]string, 0, len(platforms))
	for _, platform := range platforms {
		platformValues = append(platformValues, string(platform))
	}

	query := `
SELECT id, source_platform::text, external_id, COALESCE(sku_id, ''), title, COALESCE(subtitle, ''), url,
       COALESCE(image_url, ''), COALESCE(shop_name, ''), COALESCE(shop_type::text, ''), COALESCE(seller_name, ''), COALESCE(region, ''),
       price, currency, availability, attributes, raw_payload, first_seen_at, last_seen_at, created_at, updated_at
FROM rigel_products
WHERE source_platform::text = ANY($1)
ORDER BY updated_at DESC
LIMIT $2`
	rows, err := r.db.QueryContext(ctx, query, platformValues, limit)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	products := []model.Product{}
	for rows.Next() {
		var product model.Product
		var sourcePlatform string
		var shopType string
		var attributes []byte
		var rawPayload []byte
		if err := rows.Scan(
			&product.ID,
			&sourcePlatform,
			&product.ExternalID,
			&product.SKUID,
			&product.Title,
			&product.Subtitle,
			&product.URL,
			&product.ImageURL,
			&product.ShopName,
			&shopType,
			&product.SellerName,
			&product.Region,
			&product.Price,
			&product.Currency,
			&product.Availability,
			&attributes,
			&rawPayload,
			&product.FirstSeenAt,
			&product.LastSeenAt,
			&product.CreatedAt,
			&product.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		product.SourcePlatform = model.SourcePlatform(sourcePlatform)
		product.ShopType = model.ShopType(shopType)
		if err := decodeJSONMap(attributes, &product.Attributes); err != nil {
			return nil, err
		}
		if err := decodeJSONMap(rawPayload, &product.RawPayload); err != nil {
			return nil, err
		}
		products = append(products, product)
	}
	return products, rows.Err()
}

func (r *Repository) EnsurePart(ctx context.Context, part model.Part) (model.Part, error) {
	aliasKeywords, err := encodeJSON(part.AliasKeywords)
	if err != nil {
		return model.Part{}, err
	}

	query := `
INSERT INTO rigel_parts (category, brand, series, model, display_name, normalized_key, generation, msrp, release_year, lifecycle_status, source_confidence, alias_keywords)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (normalized_key)
DO UPDATE SET brand = EXCLUDED.brand, series = EXCLUDED.series, model = EXCLUDED.model, display_name = EXCLUDED.display_name, updated_at = NOW()
RETURNING id, created_at, updated_at`
	if err := r.db.QueryRowContext(
		ctx,
		query,
		part.Category,
		part.Brand,
		nullableString(part.Series),
		part.Model,
		part.DisplayName,
		part.NormalizedKey,
		nullableString(part.Generation),
		nullableFloat(part.MSRP),
		nullableInt(part.ReleaseYear),
		defaultLifecycleStatus(part.LifecycleStatus),
		part.SourceConfidence,
		aliasKeywords,
	).Scan(&part.ID, &part.CreatedAt, &part.UpdatedAt); err != nil {
		return model.Part{}, fmt.Errorf("ensure part: %w", err)
	}
	return part, nil
}

func (r *Repository) UpsertProductMapping(ctx context.Context, mapping model.ProductPartMapping) error {
	query := `
INSERT INTO rigel_product_part_mapping (product_id, part_id, mapping_status, match_confidence, matched_by, candidate_display_name, reason)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (product_id)
DO UPDATE SET part_id = EXCLUDED.part_id, mapping_status = EXCLUDED.mapping_status, match_confidence = EXCLUDED.match_confidence,
              matched_by = EXCLUDED.matched_by, candidate_display_name = EXCLUDED.candidate_display_name, reason = EXCLUDED.reason, updated_at = NOW()`
	if _, err := r.db.ExecContext(
		ctx,
		query,
		mapping.ProductID,
		mapping.PartID,
		defaultMappingStatus(mapping.MappingStatus),
		mapping.MatchConfidence,
		mapping.MatchedBy,
		nullableString(mapping.CandidateDisplayName),
		nullableString(mapping.Reason),
	); err != nil {
		return fmt.Errorf("upsert mapping: %w", err)
	}
	return nil
}

func (r *Repository) UpsertPartMarketSummary(ctx context.Context, summary model.PartMarketSummary) error {
	query := `
INSERT INTO rigel_part_market_summary (part_id, source_platform, latest_price, min_price, max_price, median_price, p25_price, p75_price, sample_count, window_days, last_collected_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (part_id, source_platform, window_days)
DO UPDATE SET latest_price = EXCLUDED.latest_price,
              min_price = EXCLUDED.min_price,
              max_price = EXCLUDED.max_price,
              median_price = EXCLUDED.median_price,
              p25_price = EXCLUDED.p25_price,
              p75_price = EXCLUDED.p75_price,
              sample_count = EXCLUDED.sample_count,
              last_collected_at = EXCLUDED.last_collected_at,
              updated_at = NOW()`
	if _, err := r.db.ExecContext(
		ctx,
		query,
		summary.PartID,
		summary.SourcePlatform,
		nullableFloat(summary.LatestPrice),
		nullableFloat(summary.MinPrice),
		nullableFloat(summary.MaxPrice),
		nullableFloat(summary.MedianPrice),
		nullableFloat(summary.P25Price),
		nullableFloat(summary.P75Price),
		summary.SampleCount,
		defaultWindowDays(summary.WindowDays),
		summary.LastCollectedAt,
	); err != nil {
		return fmt.Errorf("upsert part market summary: %w", err)
	}
	return nil
}

func encodeJSON(value any) ([]byte, error) {
	if value == nil {
		return []byte(`{}`), nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}
	return data, nil
}

func decodeJSONMap(data []byte, target *map[string]any) error {
	if len(data) == 0 {
		*target = map[string]any{}
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("unmarshal json map: %w", err)
	}
	if *target == nil {
		*target = map[string]any{}
	}
	return nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableFloat(value float64) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func defaultLifecycleStatus(value string) string {
	if value == "" {
		return "active"
	}
	return value
}

func defaultMappingStatus(value model.MappingStatus) string {
	if value == "" {
		return "mapped"
	}
	return string(value)
}

func defaultWindowDays(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}
