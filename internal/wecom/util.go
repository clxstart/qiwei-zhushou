package wecom

import "net/url"

func queryEscape(s string) string {
	return url.QueryEscape(s)
}
