package clientevents

import "testing"

func TestParseKeepsOnlyFixedSanitizedFields(t *testing.T) {
	t.Parallel()
	body := `{"events":[{"eventId":"event-1","type":"click","page":"/route?address=secret","detail":"line1\nline2","viewportWidth":1440,"durationMs":820,"fieldState":"nonempty","value":"password"}]}`
	batch := Parse([]byte(body))
	if batch == nil || batch.ClientApp != "route-planner" || len(batch.Events) != 1 {
		t.Fatalf("batch = %#v", batch)
	}
	event := batch.Events[0]
	if event.Type != "click" || event.Page != "/route" || event.Detail == nil || *event.Detail != "line1 line2" || event.ViewportWidth == nil || *event.ViewportWidth != 1440 {
		t.Fatalf("event = %#v", event)
	}
}

func TestParseDropsInvalidValuesAndUnknownApp(t *testing.T) {
	t.Parallel()
	batch := Parse([]byte(`{"clientApp":"my-utils","events":[{"type":"contains spaces"},{"type":"ui_error","viewportWidth":999999,"durationMs":-1,"fieldState":"secret"}]}`))
	if batch == nil || len(batch.Events) != 1 || batch.Events[0].ViewportWidth != nil || batch.Events[0].DurationMS != nil || batch.Events[0].FieldState != nil {
		t.Fatalf("batch = %#v", batch)
	}
	if Parse([]byte(`{"clientApp":"unknown","events":[{"type":"click"}]}`)) != nil {
		t.Fatal("unknown app accepted")
	}
}
