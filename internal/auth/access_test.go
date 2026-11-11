package auth

import (
	dauth "github.com/distribution/distribution/v3/registry/auth"
	"net/http/httptest"
	"testing"
)

func TestAccess(t *testing.T) {
	s, _ := service(t)
	_, secret, e := s.CreateToken("scoped", []Grant{{"team/app", "write"}})
	if e != nil {
		t.Fatal(e)
	}
	c := Controller{s}
	r := httptest.NewRequest("GET", "/v2/", nil)
	if _, e = c.Authorized(r); e == nil {
		t.Fatal("anonymous accepted")
	}
	r.SetBasicAuth("token", secret)
	if _, e = c.Authorized(r); e != nil {
		t.Fatal("valid credential denied on ping")
	}
	for _, tc := range []struct {
		typ, name, action string
		want              bool
	}{{"repository", "team/app", "push", true}, {"repository", "team/app", "pull", true}, {"repository", "team/app2", "pull", false}, {"repository", "team/app", "delete", false}, {"registry", "catalog", "*", false}, {"unknown", "team/app", "pull", false}} {
		_, e = c.Authorized(r, dauth.Access{Resource: dauth.Resource{Type: tc.typ, Name: tc.name}, Action: tc.action})
		if (e == nil) != tc.want {
			t.Errorf("access %v error=%v", tc, e)
		}
	}
	if _, e = c.Authorized(r, dauth.Access{Resource: dauth.Resource{Type: "repository", Name: "team/app"}, Action: "push"}, dauth.Access{Resource: dauth.Resource{Type: "repository", Name: "private/source"}, Action: "pull"}); e == nil {
		t.Fatal("partial mount authorized")
	}
}
