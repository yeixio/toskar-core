package automations

import "testing"

func TestConditionReadsRepairedJSON(t *testing.T) {
	n := Notification{Mode: NotifyOnCondition, Condition: &Condition{Kind: ConditionThreshold, Op: OpBelow, Value: 500}}
	for _, result := range []string{
		"The laptop is $420.\n{\"price\": \"$420\"}",
		"The laptop is $420.\n```json\n{\"price\": 420,}\n```",
	} {
		d := Decide(n, result, nil, false)
		if !d.Notify || d.Notice.Body != "The laptop is $420." {
			t.Errorf("%q: %+v", result, d)
		}
	}
	if d := Decide(n, "The laptop is $420.", nil, false); d.Notify {
		t.Error("notified without a price")
	}
	if s := ConditionSchema(n); s == nil || s.Required[0] != "price" {
		t.Fatalf("schema = %+v", s)
	}
	if ConditionSchema(Notification{Mode: NotifyAlways}) != nil {
		t.Fatal("a schema for a mode without a condition")
	}
}
