/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package errors

import (
	"fmt"
	"strings"

	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
)

const (
	levelError = "error"
)

type tfError struct {
	message string
}

type applyFailed struct {
	*tfError
}

// TerraformLog represents relevant fields of a Terraform CLI JSON-formatted log line
type TerraformLog struct {
	Level      string        `json:"@level"`
	Message    string        `json:"@message"`
	Diagnostic LogDiagnostic `json:"diagnostic"`
}

// LogDiagnostic represents relevant fields of a Terraform CLI JSON-formatted
// log line diagnostic info
type LogDiagnostic struct {
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	Detail   string `json:"detail"`
	Range    Range  `json:"range"`
}

// Range represents a line range in a Terraform workspace file
type Range struct {
	FileName string `json:"filename"`
}

func (t *tfError) Error() string {
	return t.message
}

func newTFError(message string, logs []byte) (string, *tfError) {
	tfError := &tfError{
		message: message,
	}

	tfLogs, err := parseTerraformLogs(logs)
	if err != nil {
		return err.Error(), tfError
	}

	messages := make([]string, 0, len(tfLogs))
	for _, l := range tfLogs {
		// only use error logs
		if l == nil || l.Level != levelError {
			continue
		}
		m := l.Message
		if l.Diagnostic.Severity == levelError && l.Diagnostic.Summary != "" {
			m = fmt.Sprintf("%s: %s", l.Diagnostic.Summary, l.Diagnostic.Detail)
			if len(l.Diagnostic.Range.FileName) != 0 {
				m = m + ": File name: " + l.Diagnostic.Range.FileName
			}
		}
		messages = append(messages, m)
	}
	if len(messages) == 0 {
		return "", nil
	}
	tfError.message = fmt.Sprintf("%s: %s", message, strings.Join(messages, "\n"))
	return "", tfError
}

func parseTerraformLogs(logs []byte) ([]*TerraformLog, error) {
	logLines := strings.Split(string(logs), "\n")
	tfLogs := make([]*TerraformLog, 0, len(logLines))
	for _, l := range logLines {
		log := &TerraformLog{}
		l := strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if err := jsoniter.ConfigCompatibleWithStandardLibrary.UnmarshalFromString(l, log); err != nil {
			return nil, err
		}
		tfLogs = append(tfLogs, log)
	}
	return tfLogs, nil
}

// NewApplyFailed returns a new apply failure error with given logs.
func NewApplyFailed(logs []byte) error {
	parseError, tfError := newTFError("apply failed", logs)
	if tfError == nil {
		return nil
	}
	result := &applyFailed{tfError: tfError}
	if parseError == "" {
		return result
	}
	return errors.WithMessage(result, parseError)
}

// IsApplyFailed returns whether error is due to failure of an apply operation.
func IsApplyFailed(err error) bool {
	r := &applyFailed{}
	return errors.As(err, &r)
}

type destroyFailed struct {
	*tfError
}

// NewDestroyFailed returns a new destroy failure error with given logs.
func NewDestroyFailed(logs []byte) error {
	parseError, tfError := newTFError("destroy failed", logs)
	if tfError == nil {
		return nil
	}
	result := &destroyFailed{tfError: tfError}
	if parseError == "" {
		return result
	}
	return errors.WithMessage(result, parseError)
}

// IsDestroyFailed returns whether error is due to failure of a destroy operation.
func IsDestroyFailed(err error) bool {
	r := &destroyFailed{}
	return errors.As(err, &r)
}

type refreshFailed struct {
	*tfError
}

// NewRefreshFailed returns a new destroy failure error with given logs.
func NewRefreshFailed(logs []byte) error {
	parseError, tfError := newTFError("refresh failed", logs)
	if tfError == nil {
		return nil
	}
	result := &refreshFailed{tfError: tfError}
	if parseError == "" {
		return result
	}
	return errors.WithMessage(result, parseError)
}

// IsRefreshFailed returns whether error is due to failure of a destroy operation.
func IsRefreshFailed(err error) bool {
	r := &refreshFailed{}
	return errors.As(err, &r)
}

type planFailed struct {
	*tfError
}

// NewPlanFailed returns a new destroy failure error with given logs.
func NewPlanFailed(logs []byte) error {
	parseError, tfError := newTFError("plan failed", logs)
	if tfError == nil {
		return nil
	}
	result := &planFailed{tfError: tfError}
	if parseError == "" {
		return result
	}
	return errors.WithMessage(result, parseError)
}

// IsPlanFailed returns whether error is due to failure of a destroy operation.
func IsPlanFailed(err error) bool {
	r := &planFailed{}
	return errors.As(err, &r)
}
