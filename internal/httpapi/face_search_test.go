package httpapi

import "testing"

func TestFaceSearchRouteAccess(t *testing.T) {
	if isAuthRoute("POST", "/api/galleries/gallery-1/face-search") {
		t.Fatal("public face search must not require a studio session")
	}
	if !isAuthRoute("POST", "/api/galleries/gallery-1/face-index") {
		t.Fatal("face-index backfill must require a studio session")
	}
	if !isAuthRoute("GET", "/api/galleries/gallery-1/face-index") {
		t.Fatal("unsupported face-index methods must still be authenticated before returning 405")
	}
}
