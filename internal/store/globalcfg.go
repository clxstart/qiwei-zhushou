package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
)

// GlobalConfigStore MySQL 单行全局配置。
type GlobalConfigStore struct {
	db *DB
}

func NewGlobalConfigStore(db *DB) *GlobalConfigStore {
	return &GlobalConfigStore{db: db}
}

func (s *GlobalConfigStore) Get(ctx context.Context) (*globalcfg.Config, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT delivery_notice, biz_flow_text, biz_feature_text, biz_extend_text,
		       group_name_suffix, emergency_contact, emergency_userid, trigger_scope,
		       tpl_role_announcement, tpl_tech_intro, tpl_payment_code, tpl_settle_notice,
		       payment_qr_path, updated_at
		FROM global_config WHERE id = 1`)
	var (
		cfg       globalcfg.Config
		scope     string
		updatedAt int64
	)
	err := row.Scan(
		&cfg.DeliveryNotice, &cfg.BizFlowText, &cfg.BizFeatureText, &cfg.BizExtendText,
		&cfg.GroupNameSuffix, &cfg.EmergencyContact, &cfg.EmergencyUserID, &scope,
		&cfg.TplRoleAnnouncement, &cfg.TplTechIntro, &cfg.TplPaymentCode, &cfg.TplSettleNotice,
		&cfg.PaymentQRPath, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, globalcfg.ErrNotFound
		}
		return nil, err
	}
	cfg.TriggerScope = globalcfg.TriggerScope(scope)
	cfg.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return &cfg, nil
}

func (s *GlobalConfigStore) Upsert(ctx context.Context, cfg *globalcfg.Config) error {
	_, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO global_config (
			id, delivery_notice, biz_flow_text, biz_feature_text, biz_extend_text,
			group_name_suffix, emergency_contact, emergency_userid, trigger_scope,
			tpl_role_announcement, tpl_tech_intro, tpl_payment_code, tpl_settle_notice,
			payment_qr_path, updated_at
		) VALUES (
			1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		) ON DUPLICATE KEY UPDATE
			delivery_notice=VALUES(delivery_notice),
			biz_flow_text=VALUES(biz_flow_text),
			biz_feature_text=VALUES(biz_feature_text),
			biz_extend_text=VALUES(biz_extend_text),
			group_name_suffix=VALUES(group_name_suffix),
			emergency_contact=VALUES(emergency_contact),
			emergency_userid=VALUES(emergency_userid),
			trigger_scope=VALUES(trigger_scope),
			tpl_role_announcement=VALUES(tpl_role_announcement),
			tpl_tech_intro=VALUES(tpl_tech_intro),
			tpl_payment_code=VALUES(tpl_payment_code),
			tpl_settle_notice=VALUES(tpl_settle_notice),
			payment_qr_path=VALUES(payment_qr_path),
			updated_at=VALUES(updated_at)`,
		cfg.DeliveryNotice, cfg.BizFlowText, cfg.BizFeatureText, cfg.BizExtendText,
		cfg.GroupNameSuffix, cfg.EmergencyContact, cfg.EmergencyUserID, string(cfg.TriggerScope),
		cfg.TplRoleAnnouncement, cfg.TplTechIntro, cfg.TplPaymentCode, cfg.TplSettleNotice,
		cfg.PaymentQRPath, cfg.UpdatedAt.Unix(),
	)
	return err
}
