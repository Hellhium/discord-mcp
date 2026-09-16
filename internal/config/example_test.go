package config

import "testing"

// The shipped example must stay valid: the container smoke test starts the
// server with it.
func TestExampleConfigIsValid(t *testing.T) {
	c, err := Load("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Instances) != 2 || c.Instances[0].Bot == nil || c.Instances[1].Webhook == nil {
		t.Fatalf("unexpected example: %+v", c.Instances)
	}
}
