package media

import "testing"

func TestGrantsPublicRead(t *testing.T) {
	cases := []struct {
		name   string
		policy string
		public bool
	}{
		{"no policy", "", false},
		{"anyone", `{"Statement":[{"Effect":"Allow","Principal":"*","Action":["s3:GetObject"]}]}`, true},
		{"aws wildcard", `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"*"},"Action":["s3:GetObject"]}]}`, true},
		{"aws wildcard in list", `{"Statement":[{"Effect":"Allow","Principal":{"AWS":["arn:aws:iam::1:root","*"]}}]}`, true},
		{"deny anyone", `{"Statement":[{"Effect":"Deny","Principal":"*","Action":["s3:*"]}]}`, false},
		{"one account", `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::1:role/api"}}]}`, false},
		{"unreadable", `not json`, true},
	}
	for _, tc := range cases {
		if got := grantsPublicRead(tc.policy); got != tc.public {
			t.Errorf("%s: public = %v, want %v", tc.name, got, tc.public)
		}
	}
}
