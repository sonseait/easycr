package auth

import (
	"fmt"
	dauth "github.com/distribution/distribution/v3/registry/auth"
	"net/http"
)

type Controller struct{ Service Service }
type challenge struct{}

func (challenge) Error() string { return "registry authentication required" }
func (challenge) SetHeaders(_ *http.Request, w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="EasyCR"`)
}
func init() {
	if e := dauth.Register("easycr", func(options map[string]any) (dauth.AccessController, error) {
		s, ok := options["service"].(Service)
		if !ok || s.Store == nil {
			return nil, fmt.Errorf("token service required")
		}
		return Controller{s}, nil
	}); e != nil {
		panic(e)
	}
}
func (c Controller) Authorized(r *http.Request, access ...dauth.Access) (*dauth.Grant, error) {
	u, p, ok := r.BasicAuth()
	if !ok || u != "token" {
		return nil, challenge{}
	}
	rec, e := c.Service.Authenticate(p)
	if e != nil {
		return nil, challenge{}
	}
	grant := &dauth.Grant{User: dauth.UserInfo{Name: rec.ID}}
	for _, a := range access {
		allowed := false
		switch a.Type {
		case "repository":
			allowed = Allows(rec.Grants, a.Name, a.Action)
		case "registry":
			if a.Name == "catalog" && a.Action == "*" {
				for _, g := range rec.Grants {
					if g.Repository == "*" && (g.Permission == "read" || g.Permission == "write") {
						allowed = true
					}
				}
			}
		}
		if !allowed {
			return nil, challenge{}
		}
		grant.Resources = append(grant.Resources, a.Resource)
	}
	return grant, nil
}
