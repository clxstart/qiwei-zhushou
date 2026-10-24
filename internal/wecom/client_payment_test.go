package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientListPaymentBills(t *testing.T) {
	var gotBody struct {
		BeginTime int64  `json:"begin_time"`
		EndTime   int64  `json:"end_time"`
		Cursor    string `json:"cursor"`
		Limit     int    `json:"limit"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode":      0,
				"access_token": "token-1",
				"expires_in":   7200,
			})
		case "/cgi-bin/externalpay/get_bill_list":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			if got := r.URL.Query().Get("access_token"); got != "token-1" {
				t.Fatalf("access_token = %q, want token-1", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode":     0,
				"next_cursor": "cursor-2",
				"bill_list": []map[string]any{{
					"transaction_id":   "tx-1",
					"bill_type":        0,
					"trade_state":      1,
					"pay_time":         1700000000,
					"out_trade_no":     "ot-1",
					"external_userid":  "wmABC",
					"total_fee":        500000,
					"payee_userid":     "exquisitecore",
					"payment_type":     1,
					"mch_id":           "1900000001",
					"remark":           "群wrTEST001尾款",
					"total_refund_fee": 0,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		CorpID:     "ww-test",
		CorpSecret: "secret-test",
		BaseURL:    server.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	page, err := client.ListPaymentBills(context.Background(), 100, 200, "cursor-1", 50)
	if err != nil {
		t.Fatalf("ListPaymentBills: %v", err)
	}
	if gotBody.BeginTime != 100 || gotBody.EndTime != 200 {
		t.Fatalf("time window = %+v", gotBody)
	}
	if gotBody.Cursor != "cursor-1" || gotBody.Limit != 50 {
		t.Fatalf("paging = %+v", gotBody)
	}
	if page.NextCursor != "cursor-2" {
		t.Fatalf("next cursor = %q, want cursor-2", page.NextCursor)
	}
	if len(page.Bills) != 1 {
		t.Fatalf("bills = %d, want 1", len(page.Bills))
	}
	bill := page.Bills[0]
	if bill.TransactionID != "tx-1" || bill.TotalFee != 500000 ||
		bill.PayeeUserID != "exquisitecore" || bill.PaymentType != 1 {
		t.Fatalf("bill = %+v", bill)
	}
	if bill.Remark != "群wrTEST001尾款" {
		t.Fatalf("remark = %q", bill.Remark)
	}
}

func TestClientListPaymentBills_ReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode":      0,
				"access_token": "token-1",
				"expires_in":   7200,
			})
		case "/cgi-bin/externalpay/get_bill_list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 48002,
				"errmsg":  "api forbidden",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		CorpID:     "ww-test",
		CorpSecret: "secret-test",
		BaseURL:    server.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.ListPaymentBills(context.Background(), 100, 200, "", 10)
	if !errors.Is(err, ErrAPI) {
		t.Fatalf("error = %v, want ErrAPI", err)
	}
}

func TestClientListPaymentBills_RejectsInvalidWindow(t *testing.T) {
	client, err := NewClient(ClientConfig{
		CorpID:     "ww-test",
		CorpSecret: "secret-test",
		BaseURL:    "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.ListPaymentBills(context.Background(), 200, 100, "", 10); err == nil {
		t.Fatal("expected error for reversed time window")
	}
	if _, err := client.ListPaymentBills(context.Background(), 0, 100, "", 10); err == nil {
		t.Fatal("expected error for zero begin time")
	}
}
