package delivery

import (
	"encoding/json"
	"time"
)

// 流水线步骤名（顺序固定）。
const (
	StepRoleAnnouncement = "role_announcement"
	StepDeliveryNotice   = "delivery_notice"
	StepBizFlowText      = "biz_flow_text"
	StepBizFeatureText   = "biz_feature_text"
	StepBizExtendText    = "biz_extend_text"
	StepRenameGroup      = "rename_group"
	StepTechIntro        = "tech_intro"
	StepAccountCard      = "account_card"
)

// PipelineStepOrder 执行顺序。
var PipelineStepOrder = []string{
	StepRoleAnnouncement,
	StepDeliveryNotice,
	StepBizFlowText,
	StepBizFeatureText,
	StepBizExtendText,
	StepRenameGroup,
	StepTechIntro,
	StepAccountCard,
}

// 步骤状态。
const (
	StepPending = "pending"
	StepDone    = "done"
	StepSkipped = "skipped"
	StepFailed  = "failed"
)

// StepState 单步状态。
type StepState struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// MemberSnapshot 成员快照。
type MemberSnapshot struct {
	UserID string `json:"userid"`
	Name   string `json:"name"`
	Type   int    `json:"type"` // 1 内部 2 外部
}

// GroupChat 群维度交付记录。
type GroupChat struct {
	ChatID             string           `json:"chat_id"`
	Name               string           `json:"name"`
	CreatorUserID      string           `json:"creator_userid"`
	IsExternal         bool             `json:"is_external"`
	Members            []MemberSnapshot `json:"members"`
	SelectedTechUserID string           `json:"selected_tech_userid"`
	SelectedTechName   string           `json:"selected_tech_name"`
	SelectedTechTag    string           `json:"selected_tech_tag"`
	Steps              []StepState      `json:"steps"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
}

func NewPipelineSteps() []StepState {
	out := make([]StepState, 0, len(PipelineStepOrder))
	for _, name := range PipelineStepOrder {
		out = append(out, StepState{Name: name, Status: StepPending})
	}
	return out
}

func (g *GroupChat) StepStatus(name string) string {
	for _, s := range g.Steps {
		if s.Name == name {
			return s.Status
		}
	}
	return ""
}

func (g *GroupChat) SetStep(name, status, errMsg string) {
	for i := range g.Steps {
		if g.Steps[i].Name == name {
			g.Steps[i].Status = status
			g.Steps[i].Error = errMsg
			return
		}
	}
	g.Steps = append(g.Steps, StepState{Name: name, Status: status, Error: errMsg})
}

func (g *GroupChat) StepsJSON() (string, error) {
	b, err := json.Marshal(g.Steps)
	return string(b), err
}

func ParseStepsJSON(s string) ([]StepState, error) {
	if s == "" {
		return NewPipelineSteps(), nil
	}
	var steps []StepState
	if err := json.Unmarshal([]byte(s), &steps); err != nil {
		return nil, err
	}
	return steps, nil
}

func (g *GroupChat) MembersJSON() (string, error) {
	b, err := json.Marshal(g.Members)
	return string(b), err
}

func ParseMembersJSON(s string) ([]MemberSnapshot, error) {
	if s == "" {
		return nil, nil
	}
	var m []MemberSnapshot
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}
