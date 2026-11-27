package main

import (
	"context"
	"database/sql"
	"errors"
)

func migrateUsageResets(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS usage_reset_attempts (
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 profile_id TEXT NOT NULL, connection_id TEXT NOT NULL, identity_key TEXT NOT NULL,
 credit_id TEXT NOT NULL, attempt_id TEXT NOT NULL,
 outcome TEXT NOT NULL CHECK(outcome IN ('pending','reset','alreadyRedeemed','noCredit','nothingToReset')),
 acknowledged_at INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(user_id,attempt_id));
 CREATE UNIQUE INDEX IF NOT EXISTS usage_reset_reserved_credit ON usage_reset_attempts(user_id,profile_id,connection_id,identity_key,credit_id)
 WHERE outcome IN ('pending','reset','alreadyRedeemed');
 CREATE INDEX IF NOT EXISTS usage_reset_acknowledged ON usage_reset_attempts(user_id,profile_id,connection_id,identity_key,acknowledged_at DESC)
 WHERE outcome IN ('reset','alreadyRedeemed');`)
	return err
}

type usageResetScope struct{ user, profile, connection, identity string }

func resetScope(entry *usageSession) usageResetScope {
	return usageResetScope{entry.userID, entry.view.ProfileID, entry.record.ConnectionID, entry.identityKey}
}

const usageResetScopeSQL = `user_id=? AND profile_id=? AND connection_id=? AND identity_key=?`

func (scope usageResetScope) args(extra ...any) []any {
	return append([]any{scope.user, scope.profile, scope.connection, scope.identity}, extra...)
}

type usageResetAttempt struct {
	Profile, Connection, Identity, Credit, Outcome string
}

func (attempt usageResetAttempt) matches(scope usageResetScope, credit string) bool {
	return attempt.Profile == scope.profile && attempt.Connection == scope.connection && attempt.Identity == scope.identity && attempt.Credit == credit
}

func (s *usageService) readResetAttempt(ctx context.Context, scope usageResetScope, key string) (*usageResetAttempt, error) {
	if s.resetHistoryErr != nil {
		return nil, s.resetHistoryErr
	}
	var attempt usageResetAttempt
	err := s.accounts.db.QueryRowContext(ctx, `SELECT profile_id,connection_id,identity_key,credit_id,outcome FROM usage_reset_attempts WHERE user_id=? AND attempt_id=?`, scope.user, key).Scan(&attempt.Profile, &attempt.Connection, &attempt.Identity, &attempt.Credit, &attempt.Outcome)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (s *usageService) claimResetAttempt(ctx context.Context, scope usageResetScope, req usageResetRequest) (bool, error) {
	res, err := s.accounts.db.ExecContext(ctx, `INSERT INTO usage_reset_attempts(user_id,profile_id,connection_id,identity_key,credit_id,attempt_id,outcome) VALUES(?,?,?,?,?,?,'pending') ON CONFLICT DO NOTHING`, scope.args(req.CreditID, req.AttemptID)...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *usageService) finishResetAttempt(ctx context.Context, scope usageResetScope, key, outcome string) error {
	res, err := s.accounts.db.ExecContext(ctx, `UPDATE usage_reset_attempts SET outcome=?,acknowledged_at=? WHERE `+usageResetScopeSQL+` AND attempt_id=? AND outcome='pending'`, append([]any{outcome, s.now().Unix()}, scope.args(key)...)...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("reset attempt not pending")
	}
	return err
}

func (s *usageService) loadResetHistory(entry *usageSession, view *usageResets) {
	if s.resetHistoryErr != nil {
		view.HistoryUnavailable = true
		return
	}
	scope := resetScope(entry)
	if view.Next != nil {
		var attemptID, outcome string
		err := s.accounts.db.QueryRow(`SELECT attempt_id,outcome FROM usage_reset_attempts WHERE `+usageResetScopeSQL+` AND credit_id=? AND outcome IN ('pending','reset','alreadyRedeemed')`, scope.args(view.Next.CreditID)...).Scan(&attemptID, &outcome)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// No reservation for the currently selected credit.
		case err != nil:
			view.HistoryUnavailable = true
			return
		case outcome == "pending":
			if !validResetAttemptID(attemptID) {
				view.HistoryUnavailable = true
				return
			}
			view.Next.PendingAttemptID = attemptID
		default:
			// Do not advertise another confirmation while upstream readback
			// still offers an acknowledged spent credit. Definitive no-ops
			// release the reservation without forgetting the exact attempt.
			view.Next = nil
		}
	}
	rows, err := s.accounts.db.Query(`SELECT acknowledged_at FROM usage_reset_attempts WHERE `+usageResetScopeSQL+` AND outcome IN ('reset','alreadyRedeemed') ORDER BY acknowledged_at DESC, attempt_id DESC LIMIT 100`, scope.args()...)
	if err != nil {
		view.HistoryUnavailable = true
		return
	}
	defer rows.Close()
	for rows.Next() {
		var at int64
		if err = rows.Scan(&at); err != nil {
			break
		}
		view.History = append(view.History, usageResetEvent{Kind: "used", At: at})

	}
	if err != nil || rows.Err() != nil {
		view.HistoryUnavailable = true
		view.History = view.History[:0]
	}
}
