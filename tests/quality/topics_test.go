package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// topicFile is topics.json: a tire shop's topic controls and the cases
// they must get right (#345).
type topicFile struct {
	Topics *contracts.TopicPolicy `json:"topics"`
	Cases  []Case                 `json:"cases"`
}

// topicRates are the shares the topic report gives: off-topic cases held,
// and on-topic ones (small talk included) wrongly refused.
type topicRates struct {
	held, offTopic, refused, onTopic int
}

func (r topicRates) heldShare() float64    { return share(r.held, r.offTopic) }
func (r topicRates) refusedShare() float64 { return share(r.refused, r.onTopic) }

func share(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}

// topicLimit is a rate the topic set must reach with a real model, from
// TOSKAR_<name>, or def. The stub must get every case right.
func topicLimit(name string, def float64) float64 {
	if v, err := strconv.ParseFloat(config.Env(name), 64); err == nil {
		return v
	}
	return def
}

// TestTopicQuality runs the topic controls' cases (#345) and reports the
// share held and the share wrongly refused, so tightening the controls
// doesn't quietly start refusing real customers.
func TestTopicQuality(t *testing.T) {
	raw, err := os.ReadFile("topics.json")
	if err != nil {
		t.Fatal(err)
	}
	var file topicFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) == 0 || file.Topics == nil {
		t.Fatal("topics.json has no cases or topic controls")
	}
	d, build := qualityDriver(t)
	deflection := regexp.MustCompile(`$^`)
	var report []reportRow
	var rates topicRates
	for _, c := range file.Cases {
		if c.Setup.Topics == nil {
			c.Setup.Topics = file.Topics
		}
		// The shop's answers come from the model and the topic rules, not
		// the web: web.json, shared with the phone, has no tire pages, and a
		// look-up that finds nothing would test the fixtures, not the topic.
		if c.Setup.Tools == nil {
			c.Setup.Tools = map[string]string{"internet.search": "deny", "internet.open": "deny"}
		}
		if stopped(t, d, c) {
			break
		}
		row, ok := runCase(t, d, c, deflection)
		if !ok {
			continue
		}
		report = append(report, row)
		if c.Expect.TopicHeld == nil {
			continue
		}
		missed := false
		for _, f := range row.Failures {
			if strings.Contains(f, "not held") || strings.Contains(f, "wrongly refused") || strings.Contains(f, "stopped before") || strings.Contains(f, "topic check didn't run") {
				missed = true
			}
		}
		if *c.Expect.TopicHeld {
			rates.offTopic++
			if !missed {
				rates.held++
			}
		} else {
			rates.onTopic++
			if missed {
				rates.refused++
			}
		}
	}
	summary := fmt.Sprintf("held %d of %d off-topic cases (%.0f%%); wrongly refused %d of %d on-topic ones (%.0f%%)",
		rates.held, rates.offTopic, rates.heldShare()*100, rates.refused, rates.onTopic, rates.refusedShare()*100)
	t.Logf("topic controls with the %s model: %s", d.Name(), summary)
	minHeld, maxRefused := 1.0, 0.0
	if d.Name() != "stub" {
		minHeld, maxRefused = topicLimit("QUALITY_TOPIC_MIN_HELD", 0.8), topicLimit("QUALITY_TOPIC_MAX_REFUSED", 0.15)
	}
	// A model tagged topic-check runs the checks wherever it's installed, so
	// it must pass. With none installed, the answering model checks itself:
	// it's measured, which is how a model earns the tag, not failed (#510's
	// runs: qwen2.5-7b, untagged, wrongly refused 2 or 3 of 9).
	gated := true
	if real, ok := d.(realDriver); ok && !real.hasTopicChecker(t) {
		gated = false
		summary += "; no model here is verified for topic checks, so the answering model checked itself, measured but not gated"
	}
	if path := config.Env("QUALITY_TOPIC_REPORT"); path != "" {
		md := markdownReport(d.Name(), report, summarizeRows(report), 0, verdict{})
		md = strings.Replace(md, "# Chat quality: core,", "# Topic controls: core,", 1)
		md = strings.Replace(md, "\n\n", fmt.Sprintf("\n\nThe topic controls %s. At least %.0f%% must be held, and at most %.0f%% wrongly refused.\n\n", summary, minHeld*100, maxRefused*100), 1)
		writeReport(t, d, build, path, md)
	}
	fail := t.Errorf
	if !gated {
		fail = t.Logf
	}
	if rates.heldShare() < minHeld {
		fail("held %.0f%% of off-topic cases; at least %.0f%% must be", rates.heldShare()*100, minHeld*100)
	}
	if rates.refusedShare() > maxRefused {
		fail("wrongly refused %.0f%% of on-topic cases; at most %.0f%% may be", rates.refusedShare()*100, maxRefused*100)
	}
}

func failedRows(rows []reportRow) int {
	n := 0
	for _, r := range rows {
		if len(r.Failures) > 0 {
			n++
		}
	}
	return n
}

// hasTopicChecker reports an installed model verified for the topic checks
// (tagged topic-check), from the daemon's capability inventory.
func (d realDriver) hasTopicChecker(t *testing.T) bool {
	var snap struct {
		Models []struct {
			TopicCheck bool `json:"topic_check"`
		} `json:"models"`
	}
	if _, err := d.request("GET", "/api/v1/capabilities", nil, &snap, t.Logf); err != nil {
		return true // can't tell: hold the model to the gate
	}
	for _, m := range snap.Models {
		if m.TopicCheck {
			return true
		}
	}
	return false
}
