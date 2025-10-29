// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	cbpb "cloud.google.com/go/cloudbuild/apiv1/v2/cloudbuildpb"
	"github.com/GoogleCloudPlatform/cloud-build-notifiers/lib/notifiers"
	log "github.com/golang/glog"
	"github.com/slack-go/slack"
)

const (
	webhookURLSecretName = "webhookUrl"
)

func main() {
	if err := notifiers.Main(new(slackNotifier)); err != nil {
		log.Fatalf("fatal error: %v", err)
	}
}

type slackNotifier struct {
	filter     notifiers.EventFilter
	tmpl       *template.Template
	textTmpl   *template.Template
	webhookURL string
	br         notifiers.BindingResolver
	tmplView   *notifiers.TemplateView
}

func (s *slackNotifier) SetUp(ctx context.Context, cfg *notifiers.Config, blockKitTemplate string, sg notifiers.SecretGetter, br notifiers.BindingResolver) error {
	prd, err := notifiers.MakeCELPredicate(cfg.Spec.Notification.Filter)
	if err != nil {
		return fmt.Errorf("failed to make a CEL predicate: %w", err)
	}
	s.filter = prd

	wuRef, err := notifiers.GetSecretRef(cfg.Spec.Notification.Delivery, webhookURLSecretName)
	if err != nil {
		return fmt.Errorf("failed to get Secret ref from delivery config (%v) field %q: %w", cfg.Spec.Notification.Delivery, webhookURLSecretName, err)
	}
	wuResource, err := notifiers.FindSecretResourceName(cfg.Spec.Secrets, wuRef)
	if err != nil {
		return fmt.Errorf("failed to find Secret for ref %q: %w", wuRef, err)
	}
	wu, err := sg.GetSecret(ctx, wuResource)
	if err != nil {
		return fmt.Errorf("failed to get token secret: %w", err)
	}
	s.webhookURL = wu
	tmpl, err := template.New("blockkit_template").Funcs(template.FuncMap{
		"replace": func(s, old, new string) string {
			return strings.ReplaceAll(s, old, new)
		},
		"jsonEscape": func(s string) string {
			// Escape string for JSON by marshaling it and removing surrounding quotes
			b, err := json.Marshal(s)
			if err != nil {
				// If marshaling fails, return the original string
				return s
			}
			// Remove surrounding quotes from json.Marshal result
			return string(b[1 : len(b)-1])
		},
	}).Parse(blockKitTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse blockkit template: %w", err)
	}

	s.tmpl = tmpl
	s.br = br

	// Parse text template if provided in config params
	if messageTemplate, ok := cfg.Spec.Notification.Params["messageTemplate"]; ok && messageTemplate != "" {
		messageTemplateTmpl, err := template.New("message_template").Funcs(template.FuncMap{
			"replace": func(s, old, new string) string {
				return strings.ReplaceAll(s, old, new)
			},
		}).Parse(messageTemplate)
		if err != nil {
			return fmt.Errorf("failed to parse message template: %w", err)
		}
		s.textTmpl = messageTemplateTmpl
	}

	return nil
}

func (s *slackNotifier) SendNotification(ctx context.Context, build *cbpb.Build) error {

	if !s.filter.Apply(ctx, build) {
		return nil
	}

	log.Infof("sending Slack webhook for Build %q (status: %q)", build.Id, build.Status)

	bindings, err := s.br.Resolve(ctx, nil, build)
	if err != nil {
		return fmt.Errorf("failed to resolve bindings: %w", err)
	}

	s.tmplView = &notifiers.TemplateView{
		Build:  &notifiers.BuildView{Build: build},
		Params: bindings,
	}

	msg, err := s.writeMessage()

	if err != nil {
		return fmt.Errorf("failed to write Slack message: %w", err)
	}

	return slack.PostWebhook(s.webhookURL, msg)
}

func (s *slackNotifier) writeMessage() (*slack.WebhookMessage, error) {
	build := s.tmplView.Build
	_, err := notifiers.AddUTMParams(build.LogUrl, notifiers.ChatMedium)

	if err != nil {
		return nil, fmt.Errorf("failed to add UTM params: %w", err)
	}

	var clr string
	switch build.Status {
	case cbpb.Build_SUCCESS:
		clr = "#22bb33"
	case cbpb.Build_FAILURE, cbpb.Build_INTERNAL_ERROR, cbpb.Build_TIMEOUT:
		clr = "#bb2124"
	default:
		clr = "#f0ad4e"
	}

	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, s.tmplView); err != nil {
		return nil, err
	}
	var blocks slack.Blocks

	jsonBytes := buf.Bytes()
	err = blocks.UnmarshalJSON(jsonBytes)
	if err != nil {
		// Log the problematic JSON for debugging (truncate if too long)
		jsonStr := string(jsonBytes)
		if len(jsonStr) > 500 {
			jsonStr = jsonStr[:500] + "..."
		}
		log.Errorf("failed to unmarshal templating JSON. JSON (first 500 chars): %s", jsonStr)
		return nil, fmt.Errorf("failed to unmarshal templating JSON: %w", err)
	}

	msg := &slack.WebhookMessage{Attachments: []slack.Attachment{{Color: clr, Blocks: blocks}}}

	// Set text if template is configured
	if s.textTmpl != nil {
		var textBuf bytes.Buffer
		if err := s.textTmpl.Execute(&textBuf, s.tmplView); err != nil {
			return nil, fmt.Errorf("failed to execute text template: %w", err)
		}
		msg.Text = textBuf.String()
	}

	return msg, nil
}
