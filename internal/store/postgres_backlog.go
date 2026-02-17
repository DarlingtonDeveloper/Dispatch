package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const backlogItemColumns = `id, title, description, item_type, status, domain, assigned_to, parent_id,
	impact, urgency, estimated_tokens, effort_estimate,
	priority_score, scores_source,
	model_tier, labels, one_way_door,
	stage_template, current_stage, stage_index,
	discovery_assessment,
	source, metadata, pr_url, branch_name, created_at, updated_at`

func scanBacklogItem(row pgx.Row) (*BacklogItem, error) {
	item := &BacklogItem{}
	var description, domain, assignedTo, effortEstimate sql.NullString
	var scoresSource, modelTier, source sql.NullString
	var prURL, branchName sql.NullString
	var currentStage sql.NullString
	var impact, urgency, priorityScore sql.NullFloat64
	var estimatedTokens sql.NullInt64
	var oneWayDoor sql.NullBool
	var discoveryJSON, metadataJSON []byte

	err := row.Scan(
		&item.ID, &item.Title, &description, &item.ItemType, &item.Status,
		&domain, &assignedTo, &item.ParentID,
		&impact, &urgency, &estimatedTokens, &effortEstimate,
		&priorityScore, &scoresSource,
		&modelTier, &item.Labels, &oneWayDoor,
		&item.StageTemplate, &currentStage, &item.StageIndex,
		&discoveryJSON,
		&source, &metadataJSON, &prURL, &branchName, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if currentStage.Valid {
		item.CurrentStage = currentStage.String
	}

	applyBacklogNullables(item, description, domain, assignedTo, effortEstimate,
		scoresSource, modelTier, source,
		impact, urgency, priorityScore, estimatedTokens,
		oneWayDoor, discoveryJSON, metadataJSON)
	if prURL.Valid {
		item.PRURL = prURL.String
	}
	if branchName.Valid {
		item.BranchName = branchName.String
	}
	return item, nil
}

func scanBacklogItems(rows pgx.Rows) ([]*BacklogItem, error) {
	var items []*BacklogItem
	for rows.Next() {
		item := &BacklogItem{}
		var description, domain, assignedTo, effortEstimate sql.NullString
		var scoresSource, modelTier, source sql.NullString
		var prURL, branchName sql.NullString
		var currentStage sql.NullString
		var impact, urgency, priorityScore sql.NullFloat64
		var estimatedTokens sql.NullInt64
		var oneWayDoor sql.NullBool
		var discoveryJSON, metadataJSON []byte

		if err := rows.Scan(
			&item.ID, &item.Title, &description, &item.ItemType, &item.Status,
			&domain, &assignedTo, &item.ParentID,
			&impact, &urgency, &estimatedTokens, &effortEstimate,
			&priorityScore, &scoresSource,
			&modelTier, &item.Labels, &oneWayDoor,
			&item.StageTemplate, &currentStage, &item.StageIndex,
			&discoveryJSON,
			&source, &metadataJSON, &prURL, &branchName, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if currentStage.Valid {
			item.CurrentStage = currentStage.String
		}

		applyBacklogNullables(item, description, domain, assignedTo, effortEstimate,
			scoresSource, modelTier, source,
			impact, urgency, priorityScore, estimatedTokens,
			oneWayDoor, discoveryJSON, metadataJSON)
		items = append(items, item)
	}
	return items, rows.Err()
}

func applyBacklogNullables(item *BacklogItem,
	description, domain, assignedTo, effortEstimate sql.NullString,
	scoresSource, modelTier, source sql.NullString,
	impact, urgency, priorityScore sql.NullFloat64,
	estimatedTokens sql.NullInt64,
	oneWayDoor sql.NullBool,
	discoveryJSON, metadataJSON []byte,
) {
	if description.Valid {
		item.Description = description.String
	}
	if domain.Valid {
		item.Domain = domain.String
	}
	if assignedTo.Valid {
		item.AssignedTo = assignedTo.String
	}
	if effortEstimate.Valid {
		item.EffortEstimate = effortEstimate.String
	}
	if scoresSource.Valid {
		item.ScoresSource = scoresSource.String
	}
	if modelTier.Valid {
		item.ModelTier = modelTier.String
	}
	if source.Valid {
		item.Source = source.String
	}
	if impact.Valid {
		item.Impact = &impact.Float64
	}
	if urgency.Valid {
		item.Urgency = &urgency.Float64
	}
	if priorityScore.Valid {
		item.PriorityScore = &priorityScore.Float64
	}
	if estimatedTokens.Valid {
		item.EstimatedTokens = &estimatedTokens.Int64
	}
	if oneWayDoor.Valid {
		item.OneWayDoor = oneWayDoor.Bool
	}
	if discoveryJSON != nil {
		_ = json.Unmarshal(discoveryJSON, &item.DiscoveryAssessment)
	}
	if metadataJSON != nil {
		_ = json.Unmarshal(metadataJSON, &item.Metadata)
	}
}

func (s *PostgresStore) CreateBacklogItem(ctx context.Context, item *BacklogItem) error {
	discoveryJSON, _ := json.Marshal(item.DiscoveryAssessment)
	metadataJSON, _ := json.Marshal(item.Metadata)

	return s.pool.QueryRow(ctx, `
		INSERT INTO backlog_items (title, description, item_type, status, domain, assigned_to, parent_id,
			impact, urgency, estimated_tokens, effort_estimate,
			priority_score, scores_source,
			model_tier, labels, one_way_door,
			stage_template, current_stage, stage_index,
			discovery_assessment, source, metadata, pr_url, branch_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
		RETURNING id, created_at, updated_at`,
		item.Title, nullString(item.Description), item.ItemType, item.Status,
		nullString(item.Domain), nullString(item.AssignedTo), item.ParentID,
		item.Impact, item.Urgency, item.EstimatedTokens, nullString(item.EffortEstimate),
		item.PriorityScore, nullString(item.ScoresSource),
		nullString(item.ModelTier), item.Labels, item.OneWayDoor,
		item.StageTemplate, nullString(item.CurrentStage), item.StageIndex,
		discoveryJSON, nullString(item.Source), metadataJSON, nullString(item.PRURL), nullString(item.BranchName),
	).Scan(&item.ID, &item.CreatedAt, &item.UpdatedAt)
}

func (s *PostgresStore) GetBacklogItem(ctx context.Context, id uuid.UUID) (*BacklogItem, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+backlogItemColumns+` FROM backlog_items WHERE id = $1`, id)
	item, err := scanBacklogItem(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return item, err
}

func (s *PostgresStore) ListBacklogItems(ctx context.Context, filter BacklogFilter) ([]*BacklogItem, error) {
	query := `SELECT ` + backlogItemColumns + ` FROM backlog_items WHERE 1=1`
	args := []interface{}{}
	n := 0

	if filter.Status != nil {
		n++
		query += fmt.Sprintf(" AND status = $%d", n)
		args = append(args, string(*filter.Status))
	}
	if filter.Domain != "" {
		n++
		query += fmt.Sprintf(" AND domain = $%d", n)
		args = append(args, filter.Domain)
	}
	if filter.AssignedTo != "" {
		n++
		query += fmt.Sprintf(" AND assigned_to = $%d", n)
		args = append(args, filter.AssignedTo)
	}
	if filter.ItemType != "" {
		n++
		query += fmt.Sprintf(" AND item_type = $%d", n)
		args = append(args, filter.ItemType)
	}
	if filter.ParentID != nil {
		n++
		query += fmt.Sprintf(" AND parent_id = $%d", n)
		args = append(args, *filter.ParentID)
	}

	query += " ORDER BY priority_score DESC NULLS LAST, created_at ASC"

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	n++
	query += fmt.Sprintf(" LIMIT $%d", n)
	args = append(args, limit)

	if filter.Offset > 0 {
		n++
		query += fmt.Sprintf(" OFFSET $%d", n)
		args = append(args, filter.Offset)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBacklogItems(rows)
}

func (s *PostgresStore) UpdateBacklogItem(ctx context.Context, item *BacklogItem) error {
	discoveryJSON, _ := json.Marshal(item.DiscoveryAssessment)
	metadataJSON, _ := json.Marshal(item.Metadata)

	_, err := s.pool.Exec(ctx, `
		UPDATE backlog_items SET
			title = $2, description = $3, item_type = $4, status = $5,
			domain = $6, assigned_to = $7, parent_id = $8,
			impact = $9, urgency = $10, estimated_tokens = $11, effort_estimate = $12,
			priority_score = $13, scores_source = $14,
			model_tier = $15, labels = $16, one_way_door = $17,
			stage_template = $18, current_stage = $19, stage_index = $20,
			discovery_assessment = $21, source = $22, metadata = $23,
			pr_url = $24, branch_name = $25
		WHERE id = $1`,
		item.ID, item.Title, nullString(item.Description), item.ItemType, item.Status,
		nullString(item.Domain), nullString(item.AssignedTo), item.ParentID,
		item.Impact, item.Urgency, item.EstimatedTokens, nullString(item.EffortEstimate),
		item.PriorityScore, nullString(item.ScoresSource),
		nullString(item.ModelTier), item.Labels, item.OneWayDoor,
		item.StageTemplate, nullString(item.CurrentStage), item.StageIndex,
		discoveryJSON, nullString(item.Source), metadataJSON,
		nullString(item.PRURL), nullString(item.BranchName),
	)
	return err
}

func (s *PostgresStore) DeleteBacklogItem(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM backlog_items WHERE id = $1`, id)
	return err
}

func (s *PostgresStore) GetNextBacklogItems(ctx context.Context, limit int) ([]*BacklogItem, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+backlogItemColumns+`
		FROM backlog_items
		WHERE status = 'ready'
		ORDER BY priority_score DESC NULLS LAST, created_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBacklogItems(rows)
}

// --- Dependencies ---

func (s *PostgresStore) CreateDependency(ctx context.Context, dep *BacklogDependency) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO backlog_dependencies (blocker_id, blocked_id)
		VALUES ($1, $2)
		RETURNING id, created_at`,
		dep.BlockerID, dep.BlockedID,
	).Scan(&dep.ID, &dep.CreatedAt)
}

func (s *PostgresStore) DeleteDependency(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM backlog_dependencies WHERE id = $1`, id)
	return err
}

func (s *PostgresStore) GetDependenciesForItem(ctx context.Context, itemID uuid.UUID) ([]*BacklogDependency, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, blocker_id, blocked_id, resolved_at, created_at
		FROM backlog_dependencies
		WHERE blocker_id = $1 OR blocked_id = $1
		ORDER BY created_at ASC`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []*BacklogDependency
	for rows.Next() {
		d := &BacklogDependency{}
		if err := rows.Scan(&d.ID, &d.BlockerID, &d.BlockedID, &d.ResolvedAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		deps = append(deps, d)
	}
	return deps, rows.Err()
}

func (s *PostgresStore) HasUnresolvedBlockers(ctx context.Context, itemID uuid.UUID) (bool, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM backlog_dependencies
		WHERE blocked_id = $1 AND resolved_at IS NULL`, itemID,
	).Scan(&count)
	return count > 0, err
}

func (s *PostgresStore) ResolveDependenciesForBlocker(ctx context.Context, blockerID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE backlog_dependencies SET resolved_at = NOW()
		WHERE blocker_id = $1 AND resolved_at IS NULL`, blockerID)
	return err
}

// --- Overrides ---

func (s *PostgresStore) CreateOverride(ctx context.Context, o *DispatchOverride) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO dispatch_overrides (backlog_item_id, task_id, override_type, previous_value, new_value, reason, overridden_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`,
		o.BacklogItemID, o.TaskID, o.OverrideType,
		nullString(o.PreviousValue), o.NewValue, nullString(o.Reason), o.OverriddenBy,
	).Scan(&o.ID, &o.CreatedAt)
}

// --- Autonomy ---

func (s *PostgresStore) CreateAutonomyEvent(ctx context.Context, e *AutonomyEvent) error {
	detailsJSON, _ := json.Marshal(e.Details)
	return s.pool.QueryRow(ctx, `
		INSERT INTO autonomy_events (backlog_item_id, task_id, event_type, was_autonomous, details)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`,
		e.BacklogItemID, e.TaskID, e.EventType, e.WasAutonomous, detailsJSON,
	).Scan(&e.ID, &e.CreatedAt)
}

func (s *PostgresStore) GetAutonomyMetrics(ctx context.Context, days int) ([]*AutonomyMetrics, error) {
	if days <= 0 {
		days = 30
	}
	rows, err := s.pool.Query(ctx, `
		SELECT day::TEXT, total_events, autonomous_count, overridden_count, autonomy_ratio
		FROM autonomy_rate
		LIMIT $1`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []*AutonomyMetrics
	for rows.Next() {
		m := &AutonomyMetrics{}
		if err := rows.Scan(&m.Day, &m.TotalEvents, &m.AutonomousCount, &m.OverriddenCount, &m.AutonomyRatio); err != nil {
			return nil, err
		}
		metrics = append(metrics, m)
	}
	return metrics, rows.Err()
}

// --- Median tokens helper ---

func (s *PostgresStore) GetMedianEstimatedTokens(ctx context.Context) (int64, error) {
	var median sql.NullFloat64
	err := s.pool.QueryRow(ctx, `
		SELECT PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY estimated_tokens)
		FROM backlog_items
		WHERE estimated_tokens IS NOT NULL AND estimated_tokens > 0`).Scan(&median)
	if err != nil || !median.Valid {
		return 0, err
	}
	return int64(median.Float64), nil
}

// --- Discovery Complete (transactional) ---

func (s *PostgresStore) BacklogDiscoveryComplete(ctx context.Context, itemID uuid.UUID, req *BacklogDiscoveryCompleteRequest, scoreFn ScoreFn, tierFn TierFn) (*BacklogDiscoveryCompleteResult, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Lock and read current item
	row := tx.QueryRow(ctx, `SELECT `+backlogItemColumns+` FROM backlog_items WHERE id = $1 FOR UPDATE`, itemID)
	item, err := scanBacklogItem(row)
	if err != nil {
		return nil, fmt.Errorf("lock item: %w", err)
	}

	result := &BacklogDiscoveryCompleteResult{}

	// Capture previous score
	if item.PriorityScore != nil {
		prev := *item.PriorityScore
		result.PreviousScore = &prev
	}

	// 2. Update scores from assessment
	if req.Impact != nil {
		item.Impact = req.Impact
	}
	if req.Urgency != nil {
		item.Urgency = req.Urgency
	}
	if req.EstimatedTokens != nil {
		item.EstimatedTokens = req.EstimatedTokens
	}
	if req.EffortEstimate != "" {
		item.EffortEstimate = req.EffortEstimate
	}
	if req.OneWayDoor != nil {
		item.OneWayDoor = *req.OneWayDoor
	}

	// 3. Update labels (add new, remove specified)
	if len(req.Labels) > 0 {
		existing := make(map[string]bool)
		for _, l := range item.Labels {
			existing[l] = true
		}
		for _, l := range req.Labels {
			existing[l] = true
		}
		for _, l := range req.LabelsToRemove {
			delete(existing, l)
		}
		item.Labels = make([]string, 0, len(existing))
		for l := range existing {
			item.Labels = append(item.Labels, l)
		}
	} else if len(req.LabelsToRemove) > 0 {
		remove := make(map[string]bool)
		for _, l := range req.LabelsToRemove {
			remove[l] = true
		}
		filtered := make([]string, 0, len(item.Labels))
		for _, l := range item.Labels {
			if !remove[l] {
				filtered = append(filtered, l)
			}
		}
		item.Labels = filtered
	}

	// 4. Set discovery assessment
	if req.Assessment != nil {
		item.DiscoveryAssessment = req.Assessment
	}
	item.ScoresSource = "discovery"

	// 5. Re-score priority via callback
	hasUnresolved, err := s.hasUnresolvedBlockersTx(ctx, tx, itemID)
	if err != nil {
		return nil, fmt.Errorf("check deps: %w", err)
	}

	var medianTokens int64
	var medianNull sql.NullFloat64
	err = tx.QueryRow(ctx, `
		SELECT PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY estimated_tokens)
		FROM backlog_items WHERE estimated_tokens IS NOT NULL AND estimated_tokens > 0`).Scan(&medianNull)
	if err == nil && medianNull.Valid {
		medianTokens = int64(medianNull.Float64)
	}

	if scoreFn != nil {
		score := scoreFn(item, hasUnresolved, medianTokens)
		item.PriorityScore = &score
	}

	// 6. Determine model tier via callback
	if tierFn != nil {
		item.ModelTier = tierFn(item)
	}

	// 7. Set status
	if req.Park {
		item.Status = BacklogStatusBacklog
	} else {
		item.Status = BacklogStatusPlanned
	}

	// 8. Persist updated item
	discoveryJSON, _ := json.Marshal(item.DiscoveryAssessment)
	metadataJSON, _ := json.Marshal(item.Metadata)

	_, err = tx.Exec(ctx, `
		UPDATE backlog_items SET
			title = $2, description = $3, item_type = $4, status = $5,
			domain = $6, assigned_to = $7, parent_id = $8,
			impact = $9, urgency = $10, estimated_tokens = $11, effort_estimate = $12,
			priority_score = $13, scores_source = $14,
			model_tier = $15, labels = $16, one_way_door = $17,
			stage_template = $18, current_stage = $19, stage_index = $20,
			discovery_assessment = $21, source = $22, metadata = $23,
			pr_url = $24, branch_name = $25
		WHERE id = $1`,
		item.ID, item.Title, nullString(item.Description), item.ItemType, item.Status,
		nullString(item.Domain), nullString(item.AssignedTo), item.ParentID,
		item.Impact, item.Urgency, item.EstimatedTokens, nullString(item.EffortEstimate),
		item.PriorityScore, nullString(item.ScoresSource),
		nullString(item.ModelTier), item.Labels, item.OneWayDoor,
		item.StageTemplate, nullString(item.CurrentStage), item.StageIndex,
		discoveryJSON, nullString(item.Source), metadataJSON,
		nullString(item.PRURL), nullString(item.BranchName),
	)
	if err != nil {
		return nil, fmt.Errorf("update item: %w", err)
	}

	// 9. Create discovered subtasks
	for _, sub := range req.Subtasks {
		subtask := &BacklogItem{
			Title:           sub.Title,
			Description:     sub.Description,
			ItemType:        sub.ItemType,
			Domain:          sub.Domain,
			ParentID:        &itemID,
			Impact:          sub.Impact,
			Urgency:         sub.Urgency,
			EstimatedTokens: sub.EstimatedTokens,
			EffortEstimate:  sub.EffortEstimate,
			Labels:          sub.Labels,
			Status:          BacklogStatusBacklog,
			Source:          "discovery",
			ScoresSource:    "discovery",
		}
		if subtask.ItemType == "" {
			subtask.ItemType = "task"
		}

		// Score the subtask
		if scoreFn != nil {
			score := scoreFn(subtask, false, medianTokens)
			subtask.PriorityScore = &score
		}
		if tierFn != nil {
			subtask.ModelTier = tierFn(subtask)
		}

		subDiscoveryJSON, _ := json.Marshal(subtask.DiscoveryAssessment)
		subMetadataJSON, _ := json.Marshal(subtask.Metadata)

		err = tx.QueryRow(ctx, `
			INSERT INTO backlog_items (title, description, item_type, status, domain, assigned_to, parent_id,
				impact, urgency, estimated_tokens, effort_estimate,
				priority_score, scores_source,
				model_tier, labels, one_way_door,
				stage_template, current_stage, stage_index,
				discovery_assessment, source, metadata)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
			RETURNING id, created_at, updated_at`,
			subtask.Title, nullString(subtask.Description), subtask.ItemType, subtask.Status,
			nullString(subtask.Domain), nullString(subtask.AssignedTo), subtask.ParentID,
			subtask.Impact, subtask.Urgency, subtask.EstimatedTokens, nullString(subtask.EffortEstimate),
			subtask.PriorityScore, nullString(subtask.ScoresSource),
			nullString(subtask.ModelTier), subtask.Labels, subtask.OneWayDoor,
			subtask.StageTemplate, nullString(subtask.CurrentStage), subtask.StageIndex,
			subDiscoveryJSON, nullString(subtask.Source), subMetadataJSON,
		).Scan(&subtask.ID, &subtask.CreatedAt, &subtask.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("create subtask: %w", err)
		}
		result.CreatedSubtasks = append(result.CreatedSubtasks, subtask)
	}

	// 10. Commit
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	result.Item = item
	result.UpdatedScore = item.PriorityScore
	result.ModelTier = item.ModelTier
	return result, nil
}

func (s *PostgresStore) hasUnresolvedBlockersTx(ctx context.Context, tx pgx.Tx, itemID uuid.UUID) (bool, error) {
	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM backlog_dependencies
		WHERE blocked_id = $1 AND resolved_at IS NULL`, itemID,
	).Scan(&count)
	return count > 0, err
}

// --- Stage Engine ---

func (s *PostgresStore) InitStages(ctx context.Context, itemID uuid.UUID, template []string) error {
	currentStage := ""
	if len(template) > 0 {
		currentStage = template[0]
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE backlog_items SET stage_template = $2, current_stage = $3, stage_index = 0
		WHERE id = $1`,
		itemID, template, nullString(currentStage),
	)
	return err
}

func (s *PostgresStore) GetCurrentStage(ctx context.Context, itemID uuid.UUID) (string, int, error) {
	var currentStage sql.NullString
	var stageIndex int
	err := s.pool.QueryRow(ctx, `
		SELECT current_stage, stage_index FROM backlog_items WHERE id = $1`, itemID,
	).Scan(&currentStage, &stageIndex)
	if err != nil {
		return "", 0, err
	}
	return currentStage.String, stageIndex, nil
}

func (s *PostgresStore) CreateGateCriteria(ctx context.Context, itemID uuid.UUID, stage string, criteria []string) error {
	for _, c := range criteria {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO stage_gates (backlog_item_id, stage, criterion)
			VALUES ($1, $2, $3)
			ON CONFLICT (backlog_item_id, stage, criterion) DO NOTHING`,
			itemID, stage, c,
		)
		if err != nil {
			return fmt.Errorf("create gate criterion %q: %w", c, err)
		}
	}
	return nil
}

func (s *PostgresStore) SatisfyCriterion(ctx context.Context, itemID uuid.UUID, stage, criterion, satisfiedBy string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE stage_gates SET satisfied = true, satisfied_at = NOW(), satisfied_by = $4
		WHERE backlog_item_id = $1 AND stage = $2 AND criterion ILIKE '%' || $3 || '%' AND NOT satisfied`,
		itemID, stage, criterion, nullString(satisfiedBy),
	)
	return err
}

func (s *PostgresStore) SatisfyAllCriteria(ctx context.Context, itemID uuid.UUID, stage, satisfiedBy string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE stage_gates SET satisfied = true, satisfied_at = NOW(), satisfied_by = $3
		WHERE backlog_item_id = $1 AND stage = $2 AND NOT satisfied`,
		itemID, stage, nullString(satisfiedBy),
	)
	return err
}

func (s *PostgresStore) GetGateStatus(ctx context.Context, itemID uuid.UUID, stage string) ([]GateCriterion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT criterion, satisfied, satisfied_at, satisfied_by, COALESCE(evidence, '')
		FROM stage_gates
		WHERE backlog_item_id = $1 AND stage = $2
		ORDER BY created_at ASC`, itemID, stage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var criteria []GateCriterion
	for rows.Next() {
		var gc GateCriterion
		var satisfiedBy sql.NullString
		if err := rows.Scan(&gc.Criterion, &gc.Satisfied, &gc.SatisfiedAt, &satisfiedBy, &gc.Evidence); err != nil {
			return nil, err
		}
		if satisfiedBy.Valid {
			gc.SatisfiedBy = satisfiedBy.String
		}
		criteria = append(criteria, gc)
	}
	return criteria, rows.Err()
}

func (s *PostgresStore) AllCriteriaMet(ctx context.Context, itemID uuid.UUID, stage string) (bool, error) {
	var unmet int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM stage_gates
		WHERE backlog_item_id = $1 AND stage = $2 AND NOT satisfied`, itemID, stage,
	).Scan(&unmet)
	if err != nil {
		return false, err
	}
	return unmet == 0, nil
}

// SubmitEvidence adds evidence to a gate criterion
func (s *PostgresStore) SubmitEvidence(ctx context.Context, itemID uuid.UUID, stage, criterion, evidence, submittedBy string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE stage_gates SET evidence = $4, evidence_submitted_by = $5, evidence_submitted_at = NOW()
		WHERE backlog_item_id = $1 AND stage = $2 AND criterion ILIKE '%' || $3 || '%'`,
		itemID, stage, criterion, evidence, submittedBy,
	)
	return err
}

// ResetStageToActive resets a stage back to active state for rework
func (s *PostgresStore) ResetStageToActive(ctx context.Context, itemID uuid.UUID, stage string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE stage_gates SET satisfied = FALSE, satisfied_at = NULL, satisfied_by = NULL
		WHERE backlog_item_id = $1 AND stage = $2`,
		itemID, stage,
	)
	return err
}

// GetAutonomyConfig gets the autonomy configuration for a tier
func (s *PostgresStore) GetAutonomyConfig(ctx context.Context, tier string) (*AutonomyConfig, error) {
	var config AutonomyConfig
	err := s.pool.QueryRow(ctx, `
		SELECT id, tier, auto_approve, consecutive_approvals, consecutive_corrections, updated_at
		FROM autonomy_config WHERE tier = $1`, tier).Scan(
		&config.ID, &config.Tier, &config.AutoApprove, 
		&config.ConsecutiveApprovals, &config.ConsecutiveCorrections, &config.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// UpdateAutonomyConfig updates the autonomy configuration
func (s *PostgresStore) UpdateAutonomyConfig(ctx context.Context, tier string, autoApprove bool, consecutiveApprovals, consecutiveCorrections int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE autonomy_config SET auto_approve = $2, consecutive_approvals = $3, 
		consecutive_corrections = $4, updated_at = NOW()
		WHERE tier = $1`,
		tier, autoApprove, consecutiveApprovals, consecutiveCorrections,
	)
	return err
}

// IncrementConsecutiveApprovals increments the consecutive approvals counter and returns the new count
func (s *PostgresStore) IncrementConsecutiveApprovals(ctx context.Context, tier string) (int, error) {
	var newCount int
	err := s.pool.QueryRow(ctx, `
		UPDATE autonomy_config SET consecutive_approvals = consecutive_approvals + 1, 
		updated_at = NOW() WHERE tier = $1 RETURNING consecutive_approvals`,
		tier,
	).Scan(&newCount)
	return newCount, err
}

// IncrementConsecutiveCorrections increments the consecutive corrections counter and returns the new count
func (s *PostgresStore) IncrementConsecutiveCorrections(ctx context.Context, tier string) (int, error) {
	var newCount int
	err := s.pool.QueryRow(ctx, `
		UPDATE autonomy_config SET consecutive_corrections = consecutive_corrections + 1, 
		updated_at = NOW() WHERE tier = $1 RETURNING consecutive_corrections`,
		tier,
	).Scan(&newCount)
	return newCount, err
}

// ResetAutonomyCounters resets both counters to 0
func (s *PostgresStore) ResetAutonomyCounters(ctx context.Context, tier string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE autonomy_config SET consecutive_approvals = 0, consecutive_corrections = 0, 
		updated_at = NOW() WHERE tier = $1`,
		tier,
	)
	return err
}
