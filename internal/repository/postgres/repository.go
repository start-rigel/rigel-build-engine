package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
	buildservice "github.com/rigel-labs/rigel-build-engine/internal/service/build"
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
FROM products
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
		if err := rows.Scan(&product.ID, &sourcePlatform, &product.ExternalID, &product.SKUID, &product.Title, &product.Subtitle, &product.URL, &product.ImageURL, &product.ShopName, &shopType, &product.SellerName, &product.Region, &product.Price, &product.Currency, &product.Availability, &attributes, &rawPayload, &product.FirstSeenAt, &product.LastSeenAt, &product.CreatedAt, &product.UpdatedAt); err != nil {
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

func (r *Repository) SearchParts(ctx context.Context, keyword string, limit int) ([]model.PartSearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	query := `
SELECT id, category::text, brand, model, display_name
FROM parts
WHERE ($1 = '' OR display_name ILIKE '%' || $1 || '%' OR brand ILIKE '%' || $1 || '%' OR model ILIKE '%' || $1 || '%')
ORDER BY updated_at DESC
LIMIT $2`
	rows, err := r.db.QueryContext(ctx, query, keyword, limit)
	if err != nil {
		return nil, fmt.Errorf("search parts: %w", err)
	}
	defer rows.Close()

	results := []model.PartSearchResult{}
	for rows.Next() {
		var item model.PartSearchResult
		if err := rows.Scan(&item.ID, &item.Category, &item.Brand, &item.Model, &item.DisplayName); err != nil {
			return nil, fmt.Errorf("scan part search result: %w", err)
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func (r *Repository) EnsurePart(ctx context.Context, part model.Part) (model.Part, error) {
	aliasKeywords, err := encodeJSON(part.AliasKeywords)
	if err != nil {
		return model.Part{}, err
	}
	query := `
INSERT INTO parts (category, brand, series, model, display_name, normalized_key, generation, msrp, release_year, lifecycle_status, source_confidence, alias_keywords)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (normalized_key)
DO UPDATE SET brand = EXCLUDED.brand, series = EXCLUDED.series, model = EXCLUDED.model, display_name = EXCLUDED.display_name, updated_at = NOW()
RETURNING id, created_at, updated_at`
	if err := r.db.QueryRowContext(ctx, query, part.Category, part.Brand, nullableString(part.Series), part.Model, part.DisplayName, part.NormalizedKey, nullableString(part.Generation), nullableFloat(part.MSRP), nullableInt(part.ReleaseYear), defaultLifecycleStatus(part.LifecycleStatus), part.SourceConfidence, aliasKeywords).Scan(&part.ID, &part.CreatedAt, &part.UpdatedAt); err != nil {
		return model.Part{}, fmt.Errorf("ensure part: %w", err)
	}
	return part, nil
}

func (r *Repository) UpsertProductMapping(ctx context.Context, mapping model.ProductPartMapping) error {
	query := `
INSERT INTO product_part_mapping (product_id, part_id, mapping_status, match_confidence, matched_by, candidate_display_name, reason)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (product_id)
DO UPDATE SET part_id = EXCLUDED.part_id, mapping_status = EXCLUDED.mapping_status, match_confidence = EXCLUDED.match_confidence,
              matched_by = EXCLUDED.matched_by, candidate_display_name = EXCLUDED.candidate_display_name, reason = EXCLUDED.reason, updated_at = NOW()`
	if _, err := r.db.ExecContext(ctx, query, mapping.ProductID, mapping.PartID, defaultMappingStatus(mapping.MappingStatus), mapping.MatchConfidence, mapping.MatchedBy, nullableString(mapping.CandidateDisplayName), nullableString(mapping.Reason)); err != nil {
		return fmt.Errorf("upsert mapping: %w", err)
	}
	return nil
}

func (r *Repository) UpsertPartMarketSummary(ctx context.Context, summary model.PartMarketSummary) error {
	query := `
INSERT INTO part_market_summary (part_id, source_platform, latest_price, min_price, max_price, median_price, p25_price, p75_price, sample_count, window_days, last_collected_at)
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

func (r *Repository) CreateBuildRequest(ctx context.Context, req model.BuildRequest) (model.BuildRequest, error) {
	constraints, err := encodeJSON(req.Constraints)
	if err != nil {
		return model.BuildRequest{}, err
	}
	pinnedIDs, err := encodeJSON(req.PinnedPartIDs)
	if err != nil {
		return model.BuildRequest{}, err
	}
	query := `
INSERT INTO build_requests (request_no, budget, use_case, build_mode, pinned_part_ids, constraints, status, requested_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING id, created_at, updated_at`
	if err := r.db.QueryRowContext(ctx, query, req.RequestNo, req.Budget, req.UseCase, req.BuildMode, pinnedIDs, constraints, req.Status, nullableString(req.RequestedBy)).Scan(&req.ID, &req.CreatedAt, &req.UpdatedAt); err != nil {
		return model.BuildRequest{}, fmt.Errorf("create build request: %w", err)
	}
	return req, nil
}

func (r *Repository) UpdateBuildRequestStatus(ctx context.Context, requestID model.ID, status model.BuildStatus) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE build_requests SET status = $2, updated_at = NOW() WHERE id = $1`, requestID, status); err != nil {
		return fmt.Errorf("update build request: %w", err)
	}
	return nil
}

func (r *Repository) CreateBuildResult(ctx context.Context, result model.BuildResult) (model.BuildResult, error) {
	summary, err := encodeJSON(result.Summary)
	if err != nil {
		return model.BuildResult{}, err
	}
	query := `
INSERT INTO build_results (build_request_id, result_role, scoring_profile_id, total_price, score, currency, summary, explanation_seed)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING id, created_at, updated_at`
	if err := r.db.QueryRowContext(ctx, query, result.BuildRequestID, result.ResultRole, nullableID(result.ScoringProfileID), result.TotalPrice, result.Score, result.Currency, summary, []byte(`{}`)).Scan(&result.ID, &result.CreatedAt, &result.UpdatedAt); err != nil {
		return model.BuildResult{}, fmt.Errorf("create build result: %w", err)
	}
	return result, nil
}

func (r *Repository) CreateBuildResultItems(ctx context.Context, resultID model.ID, items []model.BuildResultItem) error {
	query := `
INSERT INTO build_result_items (build_result_id, part_id, product_id, category, display_name, unit_price, quantity, source_platform, is_primary, reasons, risks, sort_order)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
	for _, item := range items {
		reasons, err := encodeJSON(item.Reasons)
		if err != nil {
			return err
		}
		risks, err := encodeJSON(item.Risks)
		if err != nil {
			return err
		}
		if _, err := r.db.ExecContext(ctx, query, resultID, nullableID(item.PartID), nullableID(item.ProductID), item.Category, item.DisplayName, item.UnitPrice, item.Quantity, nullableString(string(item.SourcePlatform)), item.IsPrimary, reasons, risks, item.SortOrder); err != nil {
			return fmt.Errorf("create build result item: %w", err)
		}
	}
	return nil
}

func (r *Repository) GetBuildAggregate(ctx context.Context, requestID model.ID) (buildservice.Response, error) {
	query := `SELECT id, request_no, budget, use_case::text, build_mode::text, status::text FROM build_requests WHERE id = $1`
	var response buildservice.Response
	if err := r.db.QueryRowContext(ctx, query, requestID).Scan(&response.BuildRequestID, &response.RequestNo, &response.Budget, &response.UseCase, &response.BuildMode, &response.Status); err != nil {
		return buildservice.Response{}, fmt.Errorf("get build request: %w", err)
	}

	resultsRows, err := r.db.QueryContext(ctx, `SELECT id, result_role::text, total_price, score, currency, summary FROM build_results WHERE build_request_id = $1 ORDER BY created_at ASC`, requestID)
	if err != nil {
		return buildservice.Response{}, fmt.Errorf("get build results: %w", err)
	}
	defer resultsRows.Close()
	for resultsRows.Next() {
		var payload buildservice.ResultPayload
		var summary []byte
		if err := resultsRows.Scan(&payload.ResultID, &payload.Role, &payload.TotalPrice, &payload.Score, &payload.Currency, &summary); err != nil {
			return buildservice.Response{}, fmt.Errorf("scan build result: %w", err)
		}
		summaryPayload := map[string]any{}
		if err := decodeJSONMap(summary, &summaryPayload); err != nil {
			return buildservice.Response{}, err
		}
		if warnings, ok := summaryPayload["warnings"].([]any); ok {
			for _, warning := range warnings {
				response.Warnings = append(response.Warnings, fmt.Sprint(warning))
			}
		}
		if findings, err := decodeCompatibilityFindings(summaryPayload["compatibility"]); err != nil {
			return buildservice.Response{}, err
		} else {
			payload.Compatibility = findings
		}

		items, err := r.getBuildItems(ctx, payload.ResultID)
		if err != nil {
			return buildservice.Response{}, err
		}
		payload.Items = items
		response.Results = append(response.Results, payload)
	}
	return response, resultsRows.Err()
}

func (r *Repository) getBuildItems(ctx context.Context, resultID string) ([]buildservice.ItemPayload, error) {
	query := `SELECT category::text, display_name, unit_price, COALESCE(source_platform::text, ''), COALESCE(product_id::text, ''), COALESCE(part_id::text, ''), reasons, risks FROM build_result_items WHERE build_result_id = $1 ORDER BY sort_order ASC`
	rows, err := r.db.QueryContext(ctx, query, resultID)
	if err != nil {
		return nil, fmt.Errorf("get build items: %w", err)
	}
	defer rows.Close()
	items := []buildservice.ItemPayload{}
	for rows.Next() {
		var item buildservice.ItemPayload
		var reasons []byte
		var risks []byte
		if err := rows.Scan(&item.Category, &item.DisplayName, &item.UnitPrice, &item.SourcePlatform, &item.ProductID, &item.PartID, &reasons, &risks); err != nil {
			return nil, fmt.Errorf("scan build item: %w", err)
		}
		if err := decodeJSONStringSlice(reasons, &item.Reasons); err != nil {
			return nil, err
		}
		if err := decodeJSONStringSlice(risks, &item.Risks); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
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

func decodeJSONStringSlice(data []byte, target *[]string) error {
	if len(data) == 0 {
		*target = []string{}
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("unmarshal string slice: %w", err)
	}
	if *target == nil {
		*target = []string{}
	}
	return nil
}

func decodeCompatibilityFindings(value any) ([]buildservice.CompatibilityFinding, error) {
	if value == nil {
		return []buildservice.CompatibilityFinding{}, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal compatibility findings: %w", err)
	}
	var findings []buildservice.CompatibilityFinding
	if err := json.Unmarshal(encoded, &findings); err != nil {
		return nil, fmt.Errorf("unmarshal compatibility findings: %w", err)
	}
	if findings == nil {
		return []buildservice.CompatibilityFinding{}, nil
	}
	return findings, nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableID(value model.ID) any {
	if value == "" {
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
