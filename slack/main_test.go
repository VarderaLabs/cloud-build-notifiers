package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"text/template"

	cbpb "cloud.google.com/go/cloudbuild/apiv1/v2/cloudbuildpb"
	"github.com/GoogleCloudPlatform/cloud-build-notifiers/lib/notifiers"
	"github.com/google/go-cmp/cmp"
	"github.com/slack-go/slack"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/protoadapt"
)

func TestWriteMessage(t *testing.T) {
	n := new(slackNotifier)

	rawPubSubMessage := `{
	  	"id": "111222333-4455-6677-8899-fa12345678",
		"status": "SUCCESS",
  		"projectId": "hello-world-123",
		"logUrl": "https://some.example.com/log/url?foo=bar\"",
		"substitutions": {
			"_GOOGLE_FUNCTION_TARGET": "helloHttp",
		}
	}`

	uo := protojson.UnmarshalOptions{
		AllowPartial:   true,
		DiscardUnknown: true,
	}

	build := new(cbpb.Build)
	bv2 := protoadapt.MessageV2Of(build)
	uo.Unmarshal([]byte(rawPubSubMessage), bv2)
	build = protoadapt.MessageV1Of(bv2).(*cbpb.Build)

	blockKitTemplate := `[
		{
		  "type": "section",
		  "text": {
			"type": "mrkdwn",
			"text": "Build {{.Build.Substitutions._GOOGLE_FUNCTION_TARGET}} Status: {{.Build.Status}}"
		  }
		},
		{
		  "type": "divider"
		},
		{
		  "type": "section",
		  "text": {
			"type": "mrkdwn",
			"text": "View Build Logs"
		  },
		  "accessory": {
			"type": "button",
			"text": {
			  "type": "plain_text",
			  "text": "Logs"
			},
			"value": "click_me_123",
			"url": "{{replace .Build.LogUrl "\"" "'"}}",
			"action_id": "button-action"
		  }
		}
	  ]`

	tmpl, err := template.New("blockkit_template").Funcs(template.FuncMap{
		"replace": func(s, old, new string) string {
			return strings.ReplaceAll(s, old, new)
		},
	}).Parse(blockKitTemplate)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	n.tmpl = tmpl
	n.tmplView = &notifiers.TemplateView{Build: &notifiers.BuildView{Build: build}}

	got, err := n.writeMessage()
	if err != nil {
		t.Fatalf("writeMessage failed: %v", err)
	}

	want := &slack.WebhookMessage{
		Attachments: []slack.Attachment{{
			Color: "#22bb33",
			Blocks: slack.Blocks{
				BlockSet: []slack.Block{
					&slack.SectionBlock{
						Type: "section",
						Text: &slack.TextBlockObject{
							Type: "mrkdwn",
							Text: "Build helloHttp Status: SUCCESS",
						},
					},
					&slack.DividerBlock{
						Type: "divider",
					},
					&slack.SectionBlock{
						Type: "section",
						Text: &slack.TextBlockObject{
							Type: "mrkdwn",
							Text: "View Build Logs",
						},
						Accessory: &slack.Accessory{ButtonElement: &slack.ButtonBlockElement{
							Type:     "button",
							Text:     &slack.TextBlockObject{Type: "plain_text", Text: "Logs"},
							ActionID: "button-action",
							URL:      "https://some.example.com/log/url?foo=bar'",
							Value:    "click_me_123",
						}},
					},
				},
			},
		}},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("writeMessage got unexpected diff: %s", diff)
	}
}

func TestWriteMessageWithTextTemplate(t *testing.T) {
	n := new(slackNotifier)

	rawPubSubMessage := `{
	  	"id": "111222333-4455-6677-8899-fa12345678",
		"status": "SUCCESS",
  		"projectId": "hello-world-123",
		"logUrl": "https://some.example.com/log/url?foo=bar\"",
		"substitutions": {
			"_GOOGLE_FUNCTION_TARGET": "helloHttp",
		}
	}`

	uo := protojson.UnmarshalOptions{
		AllowPartial:   true,
		DiscardUnknown: true,
	}

	build := new(cbpb.Build)
	bv2 := protoadapt.MessageV2Of(build)
	uo.Unmarshal([]byte(rawPubSubMessage), bv2)
	build = protoadapt.MessageV1Of(bv2).(*cbpb.Build)

	blockKitTemplate := `[
		{
		  "type": "section",
		  "text": {
			"type": "mrkdwn",
			"text": "Build {{.Build.Substitutions._GOOGLE_FUNCTION_TARGET}} Status: {{.Build.Status}}"
		  }
		}
	  ]`

	tmpl, err := template.New("blockkit_template").Funcs(template.FuncMap{
		"replace": func(s, old, new string) string {
			return strings.ReplaceAll(s, old, new)
		},
	}).Parse(blockKitTemplate)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	textTemplate := "Build {{.Build.Status}} for project {{.Build.ProjectId}}"
	textTmpl, err := template.New("text_template").Funcs(template.FuncMap{
		"replace": func(s, old, new string) string {
			return strings.ReplaceAll(s, old, new)
		},
	}).Parse(textTemplate)
	if err != nil {
		t.Fatalf("failed to parse text template: %v", err)
	}

	n.tmpl = tmpl
	n.textTmpl = textTmpl
	n.tmplView = &notifiers.TemplateView{Build: &notifiers.BuildView{Build: build}}

	got, err := n.writeMessage()
	if err != nil {
		t.Fatalf("writeMessage failed: %v", err)
	}

	want := &slack.WebhookMessage{
		Text: "Build SUCCESS for project hello-world-123",
		Attachments: []slack.Attachment{{
			Color: "#22bb33",
			Blocks: slack.Blocks{
				BlockSet: []slack.Block{
					&slack.SectionBlock{
						Type: "section",
						Text: &slack.TextBlockObject{
							Type: "mrkdwn",
							Text: "Build helloHttp Status: SUCCESS",
						},
					},
				},
			},
		}},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("writeMessage with textTmpl got unexpected diff: %s", diff)
	}
}

func TestWriteMessageWithNewlines(t *testing.T) {
	n := new(slackNotifier)

	rawPubSubMessage := `{
	  	"id": "111222333-4455-6677-8899-fa12345678",
		"status": "SUCCESS",
  		"projectId": "hello-world-123",
		"substitutions": {
			"_COMMIT_MESSAGE": "This is a commit message\nwith a newline\nand another one"
		}
	}`

	uo := protojson.UnmarshalOptions{
		AllowPartial:   true,
		DiscardUnknown: true,
	}

	build := new(cbpb.Build)
	bv2 := protoadapt.MessageV2Of(build)
	uo.Unmarshal([]byte(rawPubSubMessage), bv2)
	build = protoadapt.MessageV1Of(bv2).(*cbpb.Build)

	// Template with jsonEscape to handle newlines
	blockKitTemplate := `[
		{
		  "type": "section",
		  "text": {
			"type": "mrkdwn",
			"text": "*Commit Message:*\n{{jsonEscape .Build.Substitutions._COMMIT_MESSAGE}}"
		  }
		}
	  ]`

	tmpl, err := template.New("blockkit_template").Funcs(template.FuncMap{
		"replace": func(s, old, new string) string {
			return strings.ReplaceAll(s, old, new)
		},
		"jsonEscape": func(v interface{}) string {
			if v == nil {
				return ""
			}
			var s string
			if str, ok := v.(string); ok {
				s = str
			} else {
				s = fmt.Sprintf("%v", v)
			}
			b, err := json.Marshal(s)
			if err != nil {
				return s
			}
			return string(b[1 : len(b)-1])
		},
	}).Parse(blockKitTemplate)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	n.tmpl = tmpl
	n.tmplView = &notifiers.TemplateView{Build: &notifiers.BuildView{Build: build}}

	got, err := n.writeMessage()
	if err != nil {
		t.Fatalf("writeMessage failed: %v", err)
	}

	// Verify the message was created successfully (the newlines should be escaped)
	if got == nil {
		t.Fatal("writeMessage returned nil")
	}
	if len(got.Attachments) == 0 {
		t.Fatal("writeMessage returned no attachments")
	}
	if len(got.Attachments[0].Blocks.BlockSet) == 0 {
		t.Fatal("writeMessage returned no blocks")
	}
}

func TestWriteMessageWithMissingCommitMessage(t *testing.T) {
	n := new(slackNotifier)

	rawPubSubMessage := `{
	  	"id": "111222333-4455-6677-8899-fa12345678",
		"status": "SUCCESS",
  		"projectId": "hello-world-123",
		"substitutions": {
			"REPO_NAME": "my-repo"
		}
	}`

	uo := protojson.UnmarshalOptions{
		AllowPartial:   true,
		DiscardUnknown: true,
	}

	build := new(cbpb.Build)
	bv2 := protoadapt.MessageV2Of(build)
	uo.Unmarshal([]byte(rawPubSubMessage), bv2)
	build = protoadapt.MessageV1Of(bv2).(*cbpb.Build)

	// Template with jsonEscape on missing _COMMIT_MESSAGE
	blockKitTemplate := `[
		{
		  "type": "section",
		  "text": {
			"type": "mrkdwn",
			"text": "*Commit Message:*\n{{jsonEscape .Build.Substitutions._COMMIT_MESSAGE}}"
		  }
		}
	  ]`

	tmpl, err := template.New("blockkit_template").Funcs(template.FuncMap{
		"replace": func(s, old, new string) string {
			return strings.ReplaceAll(s, old, new)
		},
		"jsonEscape": func(v interface{}) string {
			if v == nil {
				return ""
			}
			var s string
			if str, ok := v.(string); ok {
				s = str
			} else {
				s = fmt.Sprintf("%v", v)
			}
			b, err := json.Marshal(s)
			if err != nil {
				return s
			}
			return string(b[1 : len(b)-1])
		},
	}).Parse(blockKitTemplate)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	n.tmpl = tmpl
	n.tmplView = &notifiers.TemplateView{Build: &notifiers.BuildView{Build: build}}

	got, err := n.writeMessage()
	if err != nil {
		t.Fatalf("writeMessage failed: %v", err)
	}

	// Verify the message was created successfully (missing _COMMIT_MESSAGE should result in empty string)
	if got == nil {
		t.Fatal("writeMessage returned nil")
	}
	if len(got.Attachments) == 0 {
		t.Fatal("writeMessage returned no attachments")
	}
	if len(got.Attachments[0].Blocks.BlockSet) == 0 {
		t.Fatal("writeMessage returned no blocks")
	}

	// Verify the commit message field is empty (since it was missing)
	sectionBlock, ok := got.Attachments[0].Blocks.BlockSet[0].(*slack.SectionBlock)
	if !ok {
		t.Fatalf("Expected first block to be SectionBlock, got %T", got.Attachments[0].Blocks.BlockSet[0])
	}
	if sectionBlock.Text == nil {
		t.Fatal("SectionBlock text is nil")
	}
	// The text should contain "*Commit Message:*\n" followed by empty string (from jsonEscape on nil)
	expectedText := "*Commit Message:*\n"
	if sectionBlock.Text.Text != expectedText {
		t.Errorf("Expected text to be %q, got %q", expectedText, sectionBlock.Text.Text)
	}
}
