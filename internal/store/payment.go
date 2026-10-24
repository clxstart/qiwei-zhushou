package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
)

// PaymentBillStore 对外收款流水的 MySQL 实现。
type PaymentBillStore struct {
	db *DB
}

func NewPaymentBillStore(db *DB) *PaymentBillStore {
	return &PaymentBillStore{db: db}
}

// Upsert 写入流水。已存在的记录保留人工绑定的群（chat_id 为空时不覆盖）。
func (s *PaymentBillStore) Upsert(ctx context.Context, rec *delivery.PaymentBillRecord) (bool, error) {
	if rec == nil {
		return false, errors.New("nil payment bill")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	res, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO payment_bills (
			transaction_id, chat_id, bill_type, trade_state, pay_time,
			total_fee, total_refund_fee, payee_userid, external_userid,
			payment_type, mch_id, remark, match_source, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			chat_id = IF(VALUES(chat_id) = '', chat_id, VALUES(chat_id)),
			match_source = IF(VALUES(chat_id) = '', match_source, VALUES(match_source)),
			bill_type = VALUES(bill_type),
			trade_state = VALUES(trade_state),
			pay_time = VALUES(pay_time),
			total_fee = VALUES(total_fee),
			total_refund_fee = VALUES(total_refund_fee),
			payee_userid = VALUES(payee_userid),
			external_userid = VALUES(external_userid),
			payment_type = VALUES(payment_type),
			mch_id = VALUES(mch_id),
			remark = VALUES(remark)`,
		rec.TransactionID, rec.ChatID, rec.BillType, rec.TradeState, rec.PayTime.Unix(),
		rec.TotalFee, rec.TotalRefundFee, rec.PayeeUserID, rec.ExternalUserID,
		rec.PaymentType, rec.MchID, rec.Remark, rec.MatchSource, rec.CreatedAt.Unix(),
	)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// Bind 管理员手工把某笔流水绑定到群。
func (s *PaymentBillStore) Bind(ctx context.Context, transactionID, chatID string) error {
	res, err := s.db.SQL.ExecContext(ctx, `
		UPDATE payment_bills SET chat_id = ?, match_source = ? WHERE transaction_id = ?`,
		strings.TrimSpace(chatID), delivery.PaymentMatchManual, strings.TrimSpace(transactionID))
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// 可能是把 chat_id 改成同一个值，确认记录是否存在
		var exists int
		if err := s.db.SQL.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM payment_bills WHERE transaction_id = ?`,
			strings.TrimSpace(transactionID)).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return delivery.ErrPaymentBillNotFound
		}
	}
	return nil
}

// List 按群（可选）列出流水，时间倒序。
func (s *PaymentBillStore) List(ctx context.Context, chatID string, limit int) ([]delivery.PaymentBillRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	var (
		rows *sql.Rows
		err  error
	)
	if strings.TrimSpace(chatID) == "" {
		rows, err = s.db.SQL.QueryContext(ctx, `
			SELECT transaction_id, chat_id, bill_type, trade_state, pay_time,
			       total_fee, total_refund_fee, payee_userid, external_userid,
			       payment_type, mch_id, remark, match_source, created_at
			FROM payment_bills ORDER BY pay_time DESC LIMIT ?`, limit)
	} else {
		rows, err = s.db.SQL.QueryContext(ctx, `
			SELECT transaction_id, chat_id, bill_type, trade_state, pay_time,
			       total_fee, total_refund_fee, payee_userid, external_userid,
			       payment_type, mch_id, remark, match_source, created_at
			FROM payment_bills WHERE chat_id = ? ORDER BY pay_time DESC LIMIT ?`, strings.TrimSpace(chatID), limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPaymentBills(rows)
}

// ListAll 列出全部流水（用于汇总）。
func (s *PaymentBillStore) ListAll(ctx context.Context) ([]delivery.PaymentBillRecord, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT transaction_id, chat_id, bill_type, trade_state, pay_time,
		       total_fee, total_refund_fee, payee_userid, external_userid,
		       payment_type, mch_id, remark, match_source, created_at
		FROM payment_bills ORDER BY pay_time DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPaymentBills(rows)
}

func scanPaymentBills(rows rowScanner) ([]delivery.PaymentBillRecord, error) {
	var out []delivery.PaymentBillRecord
	for rows.Next() {
		var (
			r         delivery.PaymentBillRecord
			payTime   int64
			createdAt int64
		)
		if err := rows.Scan(
			&r.TransactionID, &r.ChatID, &r.BillType, &r.TradeState, &payTime,
			&r.TotalFee, &r.TotalRefundFee, &r.PayeeUserID, &r.ExternalUserID,
			&r.PaymentType, &r.MchID, &r.Remark, &r.MatchSource, &createdAt,
		); err != nil {
			return nil, err
		}
		r.PayTime = time.Unix(payTime, 0).UTC()
		r.CreatedAt = time.Unix(createdAt, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

var _ delivery.PaymentBillStore = (*PaymentBillStore)(nil)
