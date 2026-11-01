package flights

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClassifyChoiceUsesTheEndpointAndKeyItIsGiven(t *testing.T) {
	var gotAuth, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		_, _ = w.Write([]byte(`{"model":"jev-x","answers":{"q":{"choice":"a","confidence":0.9}}}`))
	}))
	defer server.Close()
	config := MonitorConfig{Model: "jev-x", Roles: map[string]MonitorRole{
		string(RoleReviewer): {Classifiers: []MonitorClassifier{{ID: "q", Question: "Which?", Choices: []MonitorChoice{{ID: "a", Description: "A"}, {ID: "b", Description: "B"}}}}},
	}}

	got, err := ClassifyChoice(context.Background(), server.Client(), server.URL+"/custom", "secret-key", config, RoleReviewer, "some text")

	if err != nil {
		t.Fatalf("ClassifyChoice: %v", err)
	}
	if gotAuth != "Bearer secret-key" || gotPath != "/custom" {
		t.Errorf("request auth=%q path=%q", gotAuth, gotPath)
	}
	if len(got.Answers) != 1 || got.Answers[0].Choice != "a" || got.Answers[0].Confidence == nil || *got.Answers[0].Confidence != 0.9 {
		t.Errorf("classification = %+v", got)
	}
}
