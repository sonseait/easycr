package registry

import (
	"context"
	"easycr/internal/auth"
	"easycr/internal/testutil"
	"encoding/json"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"testing"
)

func TestImageStatistics(t *testing.T) {
	e, svc := fixture(t)
	_, token, err := svc.CreateToken("stats", []auth.Grant{{Repository: "team/app", Permission: "write"}})
	if err != nil {
		t.Fatal(err)
	}
	d, payload, blobs := testutil.PushImage(t, e, "team/app", "latest", token)
	info, err := e.Manifest(context.Background(), "team/app", "latest")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(info)
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	want := int64(len(payload))
	for _, blob := range blobs {
		want += blob.Size
	}
	if fields["sizeBytes"] != float64(want) || fields["lastPushedAt"] == nil {
		t.Fatalf("missing image statistics: %s", raw)
	}
	headers := map[string]string{"Accept": v1.MediaTypeImageManifest}
	testutil.Request(t, e, "HEAD", "/v2/team/app/manifests/latest", token, nil, headers)
	testutil.Request(t, e, "GET", "/v2/team/app/manifests/latest", "invalid", nil, nil)
	for _, ref := range []string{"latest", d.String()} {
		if w := testutil.Request(t, e, "GET", "/v2/team/app/manifests/"+ref, token, nil, headers); w.Code != 200 {
			t.Fatalf("get manifest: %d", w.Code)
		}
	}
	info, err = e.Manifest(context.Background(), "team/app", "latest")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(info)
	json.Unmarshal(raw, &fields)
	if fields["pullCount"] != float64(2) || fields["lastPulledAt"] == nil {
		t.Fatalf("pulls: %s", raw)
	}
	if _, err := e.Retag(context.Background(), "team/app", "alias", d.String(), false); err != nil {
		t.Fatal(err)
	}
	repo, err := e.RepositoryStatistics(context.Background(), "team/app")
	if err != nil || repo.ImageCount != 1 || repo.SizeBytes != want || repo.PullCount != 2 {
		t.Fatalf("repository: %+v %v", repo, err)
	}
	summary, err := e.Summary(context.Background())
	if err != nil || summary.ImageCount != 1 || summary.PullCount != 2 {
		t.Fatalf("summary: %+v %v", summary, err)
	}
	if len(info.Layers) != 1 || info.Layers[0].Digest != blobs[1].Digest {
		t.Fatalf("layers: %+v", info.Layers)
	}
}
