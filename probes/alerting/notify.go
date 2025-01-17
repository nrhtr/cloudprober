// Copyright 2023 The Cloudprober Authors.
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

package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cloudprober/cloudprober/common/strtemplate"
	"github.com/cloudprober/cloudprober/targets/endpoint"
	"github.com/google/shlex"
)

// AlertInfo contains information about an alert.
type AlertInfo struct {
	Name         string
	ProbeName    string
	ConditionID  string
	Target       endpoint.Endpoint
	Failures     int
	Total        int
	FailingSince time.Time
}

func alertFields(alertInfo *AlertInfo) (map[string]string, error) {
	fields := map[string]string{
		"alert":        alertInfo.Name,
		"probe":        alertInfo.ProbeName,
		"target":       alertInfo.Target.Dst(),
		"condition_id": alertInfo.ConditionID,
		"failures":     strconv.Itoa(alertInfo.Failures),
		"total":        strconv.Itoa(alertInfo.Total),
		"since":        alertInfo.FailingSince.Format(time.RFC3339),
	}

	for k, v := range alertInfo.Target.Labels {
		fields["target.label."+k] = v
	}

	alertJSON, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("error marshalling alert fields into json: %v", err)
	}

	fields["json"] = string(alertJSON)

	return fields, nil
}

func (ah *AlertHandler) notify(ep endpoint.Endpoint, ts *targetState, totalFailures int) {
	ah.l.Warningf("ALERT (%s): target (%s), failures (%d) higher than (%d) since (%v)", ah.name, ep.Name, totalFailures, ah.condition.Failures, ts.failingSince)

	ts.alerted = true
	alertInfo := &AlertInfo{
		Name:         ah.name,
		ProbeName:    ah.probeName,
		ConditionID:  ts.conditionID,
		Target:       ep,
		Failures:     totalFailures,
		Total:        int(ah.condition.Total),
		FailingSince: ts.failingSince,
	}

	if ah.notifyCh != nil {
		ah.notifyCh <- alertInfo
	}

	fields, err := alertFields(alertInfo)
	if err != nil {
		ah.l.Errorf("Error getting alert fields: %v", err)
	}

	if ah.notifyConfig != nil && ah.notifyConfig.Command != "" {
		ah.notifyCommand(context.Background(), ah.notifyConfig.Command, fields, false)
	}
}

func (ah *AlertHandler) notifyCommand(ctx context.Context, command string, fields map[string]string, dryRun bool) []string {
	res, foundAll := strtemplate.SubstituteLabels(command, fields)
	if !foundAll {
		ah.l.Warningf("couldn't substitute all labels in command: %s", command)
	}
	command = res

	cmdParts, err := shlex.Split(command)
	if err != nil {
		ah.l.Errorf("Error parsing command line (%s): %v", command, err)
		return nil
	}

	ah.l.Infof("Starting external command: %s", strings.Join(cmdParts, " "))

	cmd := exec.CommandContext(ctx, cmdParts[0], cmdParts[1:]...)

	if dryRun {
		return cmd.Args
	}

	if err = cmd.Start(); err != nil {
		ah.l.Errorf("error while starting the cmd: %s %s. Err: %v", cmd.Path, cmd.Args, err)
	}

	return nil
}
