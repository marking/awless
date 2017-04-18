/*
Copyright 2017 WALLIX

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

package driver

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/mitchellh/ioprogress"
	"github.com/wallix/awless/cloud"
	"github.com/wallix/awless/console"
	"github.com/wallix/awless/logger"
)

const (
	notFoundState = "not-found"
)

func (d *Ec2Driver) Attach_Securitygroup_DryRun(params map[string]interface{}) (interface{}, error) {
	if _, ok := params["id"]; !ok {
		return nil, errors.New("attach securitygroup: missing required params 'id'")
	}

	_, hasInstance := params["instance"]

	if !hasInstance {
		return nil, errors.New("attach securitygroup: missing 'instance' param")
	}

	d.logger.Verbose("params dry run: attach securitygroup ok")
	return nil, nil
}

func (d *Ec2Driver) Attach_Securitygroup(params map[string]interface{}) (interface{}, error) {
	instance, hasInstance := params["instance"].(string)

	switch {
	case hasInstance:
		groups, err := d.fetchInstanceSecurityGroups(instance)
		if err != nil {
			return nil, fmt.Errorf("fetching securitygroups for instance %s: %s", instance, err)
		}

		groups = append(groups, fmt.Sprint(params["id"]))
		if len(groups) == 0 {
			d.logger.Errorf("AWS instances must have at least one securitygroup")
		}
		call := &driverCall{
			d:      d,
			fn:     d.ModifyInstanceAttribute,
			logger: d.logger,
			setters: []setter{
				{val: instance, fieldPath: "InstanceID", fieldType: awsstr},
				{val: groups, fieldPath: "Groups", fieldType: awsstringslice},
			},
			desc: "attach securitygroup",
		}
		return call.execute(&ec2.ModifyInstanceAttributeInput{})
	}

	return nil, errors.New("missing 'instance' param")
}

func (d *Ec2Driver) Detach_Securitygroup_DryRun(params map[string]interface{}) (interface{}, error) {
	if _, ok := params["id"]; !ok {
		return nil, errors.New("detach securitygroup: missing required params 'id'")
	}

	_, hasInstance := params["instance"]

	if !hasInstance {
		return nil, errors.New("detach securitygroup: missing 'instance' param")
	}

	d.logger.Verbose("params dry run: detach securitygroup ok")
	return nil, nil
}

func (d *Ec2Driver) Detach_Securitygroup(params map[string]interface{}) (interface{}, error) {
	instance, hasInstance := params["instance"].(string)

	switch {
	case hasInstance:
		groups, err := d.fetchInstanceSecurityGroups(instance)
		if err != nil {
			return nil, fmt.Errorf("fetching securitygroups for instance %s: %s", instance, err)
		}

		cleaned := removeString(groups, fmt.Sprint(params["id"]))

		if len(cleaned) == 0 {
			d.logger.Errorf("AWS instances must have at least one securitygroup")
		}
		call := &driverCall{
			d:      d,
			fn:     d.ModifyInstanceAttribute,
			logger: d.logger,
			setters: []setter{
				{val: instance, fieldPath: "InstanceID", fieldType: awsstr},
				{val: cleaned, fieldPath: "Groups", fieldType: awsstringslice},
			},
			desc: "detach securitygroup",
		}
		return call.execute(&ec2.ModifyInstanceAttributeInput{})
	}

	return nil, errors.New("missing 'instance' param")
}

func (d *Ec2Driver) fetchInstanceSecurityGroups(id string) ([]string, error) {
	params := &ec2.DescribeInstanceAttributeInput{
		Attribute:  aws.String("groupSet"),
		InstanceId: aws.String(id),
	}
	resp, err := d.DescribeInstanceAttribute(params)
	if err != nil {
		return nil, err
	}

	var groups []string
	for _, g := range resp.Groups {
		groups = append(groups, aws.StringValue(g.GroupId))
	}

	return groups, nil
}

func removeString(arr []string, s string) (out []string) {
	for _, e := range arr {
		if e != s {
			out = append(out, e)
		}
	}
	return
}

func (d *IamDriver) Attach_Policy_DryRun(params map[string]interface{}) (interface{}, error) {
	if _, ok := params["arn"]; !ok {
		return nil, errors.New("attach policy: missing required params 'arn'")
	}

	_, hasUser := params["user"]
	_, hasGroup := params["group"]

	if !hasUser && !hasGroup {
		return nil, errors.New("attach policy: missing one of 'user, group' param")
	}

	d.logger.Verbose("params dry run: attach policy ok")
	return nil, nil
}

func (d *IamDriver) Attach_Policy(params map[string]interface{}) (interface{}, error) {
	user, hasUser := params["user"]
	group, hasGroup := params["group"]

	call := &driverCall{
		d:      d,
		logger: d.logger,
		setters: []setter{
			{val: params["arn"], fieldPath: "PolicyArn", fieldType: awsstr},
		},
	}

	switch {
	case hasUser:
		call.desc = "attach policy to user"
		call.fn = d.AttachUserPolicy
		call.setters = append(call.setters, setter{val: user, fieldPath: "UserName", fieldType: awsstr})
		return call.execute(&iam.AttachUserPolicyInput{})
	case hasGroup:
		call.desc = "attach policy to group"
		call.fn = d.AttachGroupPolicy
		call.setters = append(call.setters, setter{val: group, fieldPath: "GroupName", fieldType: awsstr})
		return call.execute(&iam.AttachGroupPolicyInput{})
	}

	return nil, errors.New("missing one of 'user, group' param")
}

func (d *IamDriver) Detach_Policy_DryRun(params map[string]interface{}) (interface{}, error) {
	if _, ok := params["arn"]; !ok {
		return nil, errors.New("detach policy: missing required params 'arn'")
	}

	_, hasUser := params["user"]
	_, hasGroup := params["group"]

	if !hasUser && !hasGroup {
		return nil, errors.New("detach policy: missing one of 'user, group' param")
	}

	d.logger.Verbose("params dry run: detach policy ok")
	return nil, nil
}

func (d *IamDriver) Detach_Policy(params map[string]interface{}) (interface{}, error) {
	user, hasUser := params["user"]
	group, hasGroup := params["group"]
	call := &driverCall{
		d:      d,
		logger: d.logger,
		setters: []setter{
			{val: params["arn"], fieldPath: "PolicyArn", fieldType: awsstr},
		},
	}

	switch {
	case hasUser:
		call.desc = "detach policy from user"
		call.fn = d.DetachUserPolicy
		call.setters = append(call.setters, setter{val: user, fieldPath: "UserName", fieldType: awsstr})
		return call.execute(&iam.DetachUserPolicyInput{})
	case hasGroup:
		call.desc = "detach policy from group"
		call.fn = d.DetachGroupPolicy
		call.setters = append(call.setters, setter{val: group, fieldPath: "GroupName", fieldType: awsstr})
		return call.execute(&iam.DetachGroupPolicyInput{})
	}

	return nil, errors.New("missing one of 'user, group' param")
}

func (d *IamDriver) Create_Accesskey_DryRun(params map[string]interface{}) (interface{}, error) {
	if _, ok := params["user"]; !ok {
		return nil, errors.New("create accesskey: missing required params 'user'")
	}

	d.logger.Verbose("params dry run: create accesskey ok")
	return nil, nil
}

func (d *IamDriver) Create_Accesskey(params map[string]interface{}) (interface{}, error) {
	input := &iam.CreateAccessKeyInput{}
	var err error

	// Required params
	err = setFieldWithType(params["user"], input, "UserName", awsstr)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	var output *iam.CreateAccessKeyOutput
	output, err = d.CreateAccessKey(input)

	if err != nil {
		return nil, fmt.Errorf("create accesskey: %s", err)
	}
	d.logger.ExtraVerbosef("iam.CreateAccessKey call took %s", time.Since(start))

	d.logger.Infof("Access key created. Here are the crendentials for user %s:", aws.StringValue(output.AccessKey.UserName))
	fmt.Println()
	fmt.Println(strings.Repeat("*", 64))
	fmt.Printf("aws_access_key_id = %s\n", aws.StringValue(output.AccessKey.AccessKeyId))
	fmt.Printf("aws_secret_access_key = %s\n", aws.StringValue(output.AccessKey.SecretAccessKey))
	fmt.Println(strings.Repeat("*", 64))
	fmt.Println()
	d.logger.Warning("This is your only opportunity to view the secret access keys.")
	d.logger.Warning("Save the user's new access key ID and secret access key in a safe and secure place.")
	d.logger.Warning("You will not have access to the secret keys again after this step.")

	id := aws.StringValue(output.AccessKey.AccessKeyId)

	d.logger.Infof("create accesskey '%s' done", id)
	return id, nil
}

func (d *Ec2Driver) Check_Instance_DryRun(params map[string]interface{}) (interface{}, error) {
	if _, ok := params["id"]; !ok {
		return nil, errors.New("check instance: missing required params 'id'")
	}

	states := map[string]struct{}{
		"pending":       {},
		"running":       {},
		"shutting-down": {},
		"terminated":    {},
		"stopping":      {},
		"stopped":       {},
		notFoundState:   {},
	}

	if state, ok := params["state"].(string); !ok {
		return nil, errors.New("check instance: missing required params 'state'")
	} else {
		if _, stok := states[state]; !stok {
			return nil, fmt.Errorf("check instance: invalid state '%s'", state)
		}
	}

	if _, ok := params["timeout"]; !ok {
		return nil, errors.New("check instance: missing required params 'timeout'")
	}

	if _, ok := params["timeout"].(int); !ok {
		return nil, errors.New("check instance: timeout param is not int")
	}

	input := &ec2.DescribeInstancesInput{}
	input.DryRun = aws.Bool(true)

	// Required params
	err := setFieldWithType(params["id"], input, "InstanceIds", awsstringslice)
	if err != nil {
		return nil, err
	}

	_, err = d.DescribeInstances(input)
	if awsErr, ok := err.(awserr.Error); ok {
		switch code := awsErr.Code(); {
		case code == dryRunOperation, strings.HasSuffix(code, notFound):
			id := fakeDryRunId("instance")
			d.logger.Verbose("dry run: check instance ok")
			return id, nil
		}
	}
	return nil, fmt.Errorf("dry run: check instance: %s", err)
}

func (d *Ec2Driver) Check_Instance(params map[string]interface{}) (interface{}, error) {
	input := &ec2.DescribeInstancesInput{}

	// Required params
	err := setFieldWithType(params["id"], input, "InstanceIds", awsstringslice)
	if err != nil {
		return nil, err
	}
	c := &checker{
		description: fmt.Sprintf("instance %s", params["id"]),
		timeout:     time.Duration(params["timeout"].(int)) * time.Second,
		frequency:   5 * time.Second,
		fetchFunc: func() (string, error) {
			output, err := d.DescribeInstances(input)
			if err != nil {
				if awserr, ok := err.(awserr.Error); ok {
					if awserr.Code() == "InstanceNotFound" {
						return notFoundState, nil
					}
				} else {
					return "", err
				}
			} else {
				if res := output.Reservations; len(res) > 0 {
					if instances := output.Reservations[0].Instances; len(instances) > 0 {
						for _, inst := range instances {
							if aws.StringValue(inst.InstanceId) == params["id"] {
								return aws.StringValue(inst.State.Name), nil
							}
						}
					}
				}
			}
			return notFoundState, nil
		},
		expect: fmt.Sprint(params["state"]),
		logger: d.logger,
	}
	return nil, c.check()
}

func (d *Ec2Driver) Check_Securitygroup_DryRun(params map[string]interface{}) (interface{}, error) {
	if _, ok := params["id"]; !ok {
		return nil, errors.New("check securitygroup: missing required params 'id'")
	}

	states := map[string]struct{}{
		"unused": {},
	}

	if state, ok := params["state"].(string); !ok {
		return nil, errors.New("check securitygroup: missing required params 'state'")
	} else {
		if _, stok := states[state]; !stok {
			return nil, fmt.Errorf("check securitygroup: invalid state '%s'", state)
		}
	}

	if _, ok := params["timeout"]; !ok {
		return nil, errors.New("check securitygroup: missing required params 'timeout'")
	}

	if _, ok := params["timeout"].(int); !ok {
		return nil, errors.New("check securitygroup: timeout param is not int")
	}
	d.logger.Verbose("dry run: check instance ok")
	return nil, nil
}

func (d *Ec2Driver) Check_Securitygroup(params map[string]interface{}) (interface{}, error) {
	input := &ec2.DescribeNetworkInterfacesInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("group-id"), Values: []*string{aws.String(fmt.Sprint(params["id"]))}},
		},
	}

	c := &checker{
		description: fmt.Sprintf("securitygroup %s", params["id"]),
		timeout:     time.Duration(params["timeout"].(int)) * time.Second,
		frequency:   5 * time.Second,
		fetchFunc: func() (string, error) {
			output, err := d.DescribeNetworkInterfaces(input)
			if err != nil {
				return "", err
			}
			if len(output.NetworkInterfaces) == 0 {
				return "unused", nil
			}
			var niIds []string
			for _, ni := range output.NetworkInterfaces {
				niIds = append(niIds, aws.StringValue(ni.NetworkInterfaceId))
			}
			return fmt.Sprintf("used by %s", strings.Join(niIds, ", ")), nil
		},
		expect: fmt.Sprint(params["state"]),
		logger: d.logger,
	}
	return nil, c.check()
}

func (d *Elbv2Driver) Check_Loadbalancer_DryRun(params map[string]interface{}) (interface{}, error) {
	if _, ok := params["id"]; !ok {
		return nil, errors.New("check loadbalancer: missing required params 'id'")
	}

	states := map[string]struct{}{
		"provisioning": {},
		"active":       {},
		"failed":       {},
		notFoundState:  {},
	}

	if state, ok := params["state"].(string); !ok {
		return nil, errors.New("check loadbalancer: missing required params 'state'")
	} else {
		if _, stok := states[state]; !stok {
			return nil, fmt.Errorf("check loadbalancer: invalid state '%s'", state)
		}
	}

	if _, ok := params["timeout"]; !ok {
		return nil, errors.New("check loadbalancer: missing required params 'timeout'")
	}

	d.logger.Verbose("params dry run: check loadbalancer ok")
	return nil, nil
}

func (d *Elbv2Driver) Check_Loadbalancer(params map[string]interface{}) (interface{}, error) {
	input := &elbv2.DescribeLoadBalancersInput{}

	// Required params
	err := setFieldWithType(params["id"], input, "LoadBalancerArns", awsstringslice)
	if err != nil {
		return nil, err
	}
	c := &checker{
		description: fmt.Sprintf("loadbalancer %s", params["id"]),
		timeout:     time.Duration(params["timeout"].(int)) * time.Second,
		frequency:   5 * time.Second,
		fetchFunc: func() (string, error) {
			output, err := d.DescribeLoadBalancers(input)
			if err != nil {
				if awserr, ok := err.(awserr.Error); ok {
					if awserr.Code() == "LoadBalancerNotFound" {
						return notFoundState, nil
					}
				} else {
					return "", err
				}
			} else {
				for _, lb := range output.LoadBalancers {
					if aws.StringValue(lb.LoadBalancerArn) == params["id"] {
						return aws.StringValue(lb.State.Code), nil
					}
				}
			}
			return notFoundState, nil
		},
		expect: fmt.Sprint(params["state"]),
		logger: d.logger,
	}
	return nil, c.check()
}

func (d *Ec2Driver) Create_Tag_DryRun(params map[string]interface{}) (interface{}, error) {
	input := &ec2.CreateTagsInput{}
	input.DryRun = aws.Bool(true)
	var err error

	// Required params
	err = setFieldWithType(params["resource"], input, "Resources", awsstringslice)
	if err != nil {
		return nil, err
	}
	input.Tags = []*ec2.Tag{{Key: aws.String(fmt.Sprint(params["key"])), Value: aws.String(fmt.Sprint(params["value"]))}}

	_, err = d.CreateTags(input)
	if awsErr, ok := err.(awserr.Error); ok {
		switch code := awsErr.Code(); {
		case code == dryRunOperation, strings.HasSuffix(code, notFound):
			id := fakeDryRunId("tag")
			d.logger.Verbosef("dry run: create tag '%s=%s' ok", params["key"], params["value"])
			return id, nil
		}
	}

	return nil, fmt.Errorf("dry run: create tag: %s", err)
}

func (d *Ec2Driver) Create_Tag(params map[string]interface{}) (interface{}, error) {
	input := &ec2.CreateTagsInput{}
	var err error

	// Required params
	err = setFieldWithType(params["resource"], input, "Resources", awsstringslice)
	if err != nil {
		return nil, err
	}
	input.Tags = []*ec2.Tag{{Key: aws.String(fmt.Sprint(params["key"])), Value: aws.String(fmt.Sprint(params["value"]))}}

	start := time.Now()
	var output *ec2.CreateTagsOutput
	output, err = d.CreateTags(input)
	if err != nil {
		return nil, fmt.Errorf("create tag: %s", err)
	}
	d.logger.ExtraVerbosef("ec2.CreateTags call took %s", time.Since(start))
	d.logger.Infof("create tag '%s=%s' on '%s' done", params["key"], params["value"], params["resource"])
	return output, nil
}

func (d *Ec2Driver) Delete_Tag_DryRun(params map[string]interface{}) (interface{}, error) {
	input := &ec2.DeleteTagsInput{}
	input.DryRun = aws.Bool(true)
	var err error

	// Required params
	err = setFieldWithType(params["resource"], input, "Resources", awsstringslice)
	if err != nil {
		return nil, err
	}
	input.Tags = []*ec2.Tag{{Key: aws.String(fmt.Sprint(params["key"])), Value: aws.String(fmt.Sprint(params["value"]))}}

	_, err = d.DeleteTags(input)
	if awsErr, ok := err.(awserr.Error); ok {
		switch code := awsErr.Code(); {
		case code == dryRunOperation, strings.HasSuffix(code, notFound):
			id := fakeDryRunId("tag")
			d.logger.Verbosef("dry run: delete tag '%s=%s' ok", params["key"], params["value"])
			return id, nil
		}
	}

	return nil, fmt.Errorf("dry run: delete tag: %s", err)
}

func (d *Ec2Driver) Delete_Tag(params map[string]interface{}) (interface{}, error) {
	input := &ec2.DeleteTagsInput{}
	var err error

	// Required params
	err = setFieldWithType(params["resource"], input, "Resources", awsstringslice)
	if err != nil {
		return nil, err
	}
	input.Tags = []*ec2.Tag{{Key: aws.String(fmt.Sprint(params["key"])), Value: aws.String(fmt.Sprint(params["value"]))}}

	start := time.Now()
	var output *ec2.DeleteTagsOutput
	output, err = d.DeleteTags(input)
	if err != nil {
		return nil, fmt.Errorf("delete tag: %s", err)
	}
	d.logger.ExtraVerbosef("ec2.DeleteTags call took %s", time.Since(start))
	d.logger.Infof("delete tag '%s=%s' on '%s' done", params["key"], params["value"], params["resource"])
	return output, nil
}

func (d *Ec2Driver) Create_Keypair_DryRun(params map[string]interface{}) (interface{}, error) {
	input := &ec2.ImportKeyPairInput{}

	input.DryRun = aws.Bool(true)
	err := setFieldWithType(params["name"], input, "KeyName", awsstr)
	if err != nil {
		return nil, err
	}

	if params["name"] == "" {
		return nil, fmt.Errorf("dry run: saving private key: empty 'name' parameter")
	}

	const keyDirEnv = "__AWLESS_KEYS_DIR"
	keyDir := os.Getenv(keyDirEnv)
	if keyDir == "" {
		return nil, fmt.Errorf("dry run: saving private key: empty env var '%s'", keyDirEnv)
	}

	privKeyPath := filepath.Join(keyDir, fmt.Sprint(params["name"])+".pem")
	_, err = os.Stat(privKeyPath)
	if err == nil {
		return nil, fmt.Errorf("dry run: saving private key: file already exists at path: %s", privKeyPath)
	}

	return nil, nil
}

func (d *Ec2Driver) Create_Keypair(params map[string]interface{}) (interface{}, error) {
	input := &ec2.ImportKeyPairInput{}
	err := setFieldWithType(params["name"], input, "KeyName", awsstr)
	if err != nil {
		return nil, err
	}

	d.logger.Info("Generating locally a RSA 4096 bits keypair...")
	pub, priv, err := console.GenerateSSHKeyPair(4096)
	if err != nil {
		return nil, fmt.Errorf("generating key: %s", err)
	}
	privKeyPath := filepath.Join(os.Getenv("__AWLESS_KEYS_DIR"), fmt.Sprint(params["name"])+".pem")
	_, err = os.Stat(privKeyPath)
	if err == nil {
		return nil, fmt.Errorf("saving private key: file already exists at path: %s", privKeyPath)
	}
	err = ioutil.WriteFile(privKeyPath, priv, 0400)
	if err != nil {
		return nil, fmt.Errorf("saving private key: %s", err)
	}
	d.logger.Infof("4096 RSA keypair generated locally and stored in '%s'", privKeyPath)
	input.PublicKeyMaterial = pub

	output, err := d.ImportKeyPair(input)
	if err != nil {
		return nil, fmt.Errorf("create key: %s", err)
	}
	id := aws.StringValue(output.KeyName)
	d.logger.Infof("create keypair '%s' done", id)
	return aws.StringValue(output.KeyName), nil
}

func (d *Ec2Driver) Update_Securitygroup_DryRun(params map[string]interface{}) (interface{}, error) {
	ipPerms, err := buildIpPermissionsFromParams(params)
	if err != nil {
		return nil, err
	}
	var input interface{}
	if action, ok := params["inbound"].(string); ok {
		switch action {
		case "authorize":
			input = &ec2.AuthorizeSecurityGroupIngressInput{DryRun: aws.Bool(true), IpPermissions: ipPerms}
		case "revoke":
			input = &ec2.RevokeSecurityGroupIngressInput{DryRun: aws.Bool(true), IpPermissions: ipPerms}
		default:
			return nil, fmt.Errorf("'inbound' parameter expect 'authorize' or 'revoke', got %s", action)
		}
	}
	if action, ok := params["outbound"].(string); ok {
		switch action {
		case "authorize":
			input = &ec2.AuthorizeSecurityGroupEgressInput{DryRun: aws.Bool(true), IpPermissions: ipPerms}
		case "revoke":
			input = &ec2.RevokeSecurityGroupEgressInput{DryRun: aws.Bool(true), IpPermissions: ipPerms}
		default:
			return nil, fmt.Errorf("'outbound' parameter expect 'authorize' or 'revoke', got %s", action)
		}
	}
	if input == nil {
		return nil, fmt.Errorf("expect either 'inbound' or 'outbound' parameter")
	}

	// Required params
	err = setFieldWithType(params["id"], input, "GroupId", awsstr)
	if err != nil {
		return nil, err
	}

	switch ii := input.(type) {
	case *ec2.AuthorizeSecurityGroupIngressInput:
		_, err = d.AuthorizeSecurityGroupIngress(ii)
	case *ec2.RevokeSecurityGroupIngressInput:
		_, err = d.RevokeSecurityGroupIngress(ii)
	case *ec2.AuthorizeSecurityGroupEgressInput:
		_, err = d.AuthorizeSecurityGroupEgress(ii)
	case *ec2.RevokeSecurityGroupEgressInput:
		_, err = d.RevokeSecurityGroupEgress(ii)
	}
	if awsErr, ok := err.(awserr.Error); ok {
		switch code := awsErr.Code(); {
		case code == dryRunOperation, strings.HasSuffix(code, notFound):
			d.logger.Verbose("dry run: update securitygroup ok")
			return nil, nil
		}
	}
	return nil, fmt.Errorf("dry run: update securitygroup: %s", err)
}

func (d *Ec2Driver) Update_Securitygroup(params map[string]interface{}) (interface{}, error) {
	ipPerms, err := buildIpPermissionsFromParams(params)
	if err != nil {
		return nil, err
	}
	var input interface{}
	if action, ok := params["inbound"].(string); ok {
		switch action {
		case "authorize":
			input = &ec2.AuthorizeSecurityGroupIngressInput{IpPermissions: ipPerms}
		case "revoke":
			input = &ec2.RevokeSecurityGroupIngressInput{IpPermissions: ipPerms}
		default:
			return nil, fmt.Errorf("'inbound' parameter expect 'authorize' or 'revoke', got %s", action)
		}
	}
	if action, ok := params["outbound"].(string); ok {
		switch action {
		case "authorize":
			input = &ec2.AuthorizeSecurityGroupEgressInput{IpPermissions: ipPerms}
		case "revoke":
			input = &ec2.RevokeSecurityGroupEgressInput{IpPermissions: ipPerms}
		default:
			return nil, fmt.Errorf("'outbound' parameter expect 'authorize' or 'revoke', got %s", action)
		}
	}
	if input == nil {
		return nil, fmt.Errorf("expect either 'inbound' or 'outbound' parameter")
	}

	// Required params
	err = setFieldWithType(params["id"], input, "GroupId", awsstr)
	if err != nil {
		return nil, err
	}

	var output interface{}
	switch ii := input.(type) {
	case *ec2.AuthorizeSecurityGroupIngressInput:
		output, err = d.AuthorizeSecurityGroupIngress(ii)
	case *ec2.RevokeSecurityGroupIngressInput:
		output, err = d.RevokeSecurityGroupIngress(ii)
	case *ec2.AuthorizeSecurityGroupEgressInput:
		output, err = d.AuthorizeSecurityGroupEgress(ii)
	case *ec2.RevokeSecurityGroupEgressInput:
		output, err = d.RevokeSecurityGroupEgress(ii)
	}
	if err != nil {
		return nil, fmt.Errorf("update securitygroup: %s", err)
	}

	d.logger.Info("update securitygroup done")
	return output, nil
}

func (d *S3Driver) Create_S3object_DryRun(params map[string]interface{}) (interface{}, error) {
	if _, ok := params["bucket"]; !ok {
		return nil, errors.New("create s3object: missing required params 'bucket'")
	}

	if _, ok := params["file"].(string); !ok {
		return nil, errors.New("create s3object: missing required string params 'file'")
	}

	stat, err := os.Stat(params["file"].(string))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot find file '%s'", params["file"])
	}
	if err != nil {
		return nil, err
	}
	if stat.IsDir() {
		return nil, fmt.Errorf("'%s' is a directory", params["file"])
	}

	d.logger.Verbose("params dry run: create s3object ok")
	return nil, nil
}

type progressReadSeeker struct {
	file   *os.File
	reader *ioprogress.Reader
}

func newProgressReader(f *os.File) (*progressReadSeeker, error) {
	finfo, err := f.Stat()
	if err != nil {
		return nil, err
	}

	draw := func(progress, total int64) string {
		// &s3.PutObjectInput.Body will be read twice
		// once in memory and a second time for the HTTP upload
		// here we only display for the actual HTTP upload
		if progress > total {
			return ioprogress.DrawTextFormatBytes(progress/2, total)
		}
		return ""
	}

	reader := &ioprogress.Reader{
		DrawFunc: ioprogress.DrawTerminalf(os.Stdout, draw),
		Reader:   f,
		Size:     finfo.Size(),
	}

	return &progressReadSeeker{file: f, reader: reader}, nil
}

func (pr *progressReadSeeker) Read(p []byte) (int, error) {
	return pr.reader.Read(p)
}

func (pr *progressReadSeeker) Seek(offset int64, whence int) (int64, error) {
	return pr.file.Seek(offset, whence)
}

func (d *S3Driver) Create_S3object(params map[string]interface{}) (interface{}, error) {
	input := &s3.PutObjectInput{}

	f, err := os.Open(params["file"].(string))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	progressR, err := newProgressReader(f)
	if err != nil {
		return nil, err
	}
	input.Body = progressR

	var fileName string
	if n, ok := params["name"].(string); ok && n != "" {
		fileName = n
	} else {
		_, fileName = filepath.Split(f.Name())
	}
	input.Key = aws.String(fileName)

	// Required params
	err = setFieldWithType(params["bucket"], input, "Bucket", awsstr)
	if err != nil {
		return nil, err
	}

	d.logger.Infof("uploading '%s'", fileName)

	output, err := d.PutObject(input)
	if err != nil {
		return nil, fmt.Errorf("create s3object: %s", err)
	}

	d.logger.Info("create s3object done")
	return output, nil
}

func (d *Route53Driver) Create_Record_DryRun(params map[string]interface{}) (interface{}, error) {
	if _, ok := params["zone"]; !ok {
		return nil, errors.New("create record: missing required params 'zone'")
	}

	if _, ok := params["name"]; !ok {
		return nil, errors.New("create record: missing required params 'name'")
	}

	if _, ok := params["type"]; !ok {
		return nil, errors.New("create record: missing required params 'type'")
	}

	if _, ok := params["value"]; !ok {
		return nil, errors.New("create record: missing required params 'value'")
	}

	if _, ok := params["ttl"]; !ok {
		return nil, errors.New("create record: missing required params 'ttl'")
	}

	d.logger.Verbose("params dry run: create record ok")
	return nil, nil
}

func (d *Route53Driver) Create_Record(params map[string]interface{}) (interface{}, error) {
	input := &route53.ChangeResourceRecordSetsInput{}
	var err error
	// Required params
	err = setFieldWithType(params["zone"], input, "HostedZoneId", awsstr)
	if err != nil {
		return nil, err
	}
	resourceRecord := &route53.ResourceRecord{}
	change := &route53.Change{ResourceRecordSet: &route53.ResourceRecordSet{ResourceRecords: []*route53.ResourceRecord{resourceRecord}}}
	input.ChangeBatch = &route53.ChangeBatch{Changes: []*route53.Change{change}}
	err = setFieldWithType("CREATE", change, "Action", awsstr)
	if err != nil {
		return nil, err
	}
	err = setFieldWithType(params["name"], change, "ResourceRecordSet.Name", awsstr)
	if err != nil {
		return nil, err
	}
	err = setFieldWithType(params["type"], change, "ResourceRecordSet.Type", awsstr)
	if err != nil {
		return nil, err
	}
	err = setFieldWithType(params["ttl"], change, "ResourceRecordSet.TTL", awsint64)
	if err != nil {
		return nil, err
	}
	err = setFieldWithType(params["value"], resourceRecord, "Value", awsstr)
	if err != nil {
		return nil, err
	}

	// Extra params
	if _, ok := params["comment"]; ok {
		err = setFieldWithType(params["comment"], input, "ChangeBatch.Comment", awsstr)
		if err != nil {
			return nil, err
		}
	}

	start := time.Now()
	var output *route53.ChangeResourceRecordSetsOutput
	output, err = d.ChangeResourceRecordSets(input)

	if err != nil {
		return nil, fmt.Errorf("create record: %s", err)
	}
	d.logger.ExtraVerbosef("route53.ChangeResourceRecordSets call took %s", time.Since(start))
	d.logger.Info("create record done")
	return aws.StringValue(output.ChangeInfo.Id), nil
}

func (d *Route53Driver) Delete_Record_DryRun(params map[string]interface{}) (interface{}, error) {
	if _, ok := params["zone"]; !ok {
		return nil, errors.New("delete record: missing required params 'zone'")
	}

	if _, ok := params["name"]; !ok {
		return nil, errors.New("delete record: missing required params 'name'")
	}

	if _, ok := params["type"]; !ok {
		return nil, errors.New("delete record: missing required params 'type'")
	}

	if _, ok := params["value"]; !ok {
		return nil, errors.New("delete record: missing required params 'value'")
	}

	if _, ok := params["ttl"]; !ok {
		return nil, errors.New("delete record: missing required params 'value'")
	}

	d.logger.Verbose("params dry run: delete record ok")
	return nil, nil
}

func (d *Route53Driver) Delete_Record(params map[string]interface{}) (interface{}, error) {
	input := &route53.ChangeResourceRecordSetsInput{}
	var err error
	// Required params
	err = setFieldWithType(params["zone"], input, "HostedZoneId", awsstr)
	if err != nil {
		return nil, err
	}
	resourceRecord := &route53.ResourceRecord{}
	change := &route53.Change{ResourceRecordSet: &route53.ResourceRecordSet{ResourceRecords: []*route53.ResourceRecord{resourceRecord}}}
	input.ChangeBatch = &route53.ChangeBatch{Changes: []*route53.Change{change}}
	err = setFieldWithType("DELETE", change, "Action", awsstr)
	if err != nil {
		return nil, err
	}
	err = setFieldWithType(params["name"], change, "ResourceRecordSet.Name", awsstr)
	if err != nil {
		return nil, err
	}
	err = setFieldWithType(params["type"], change, "ResourceRecordSet.Type", awsstr)
	if err != nil {
		return nil, err
	}
	err = setFieldWithType(params["ttl"], change, "ResourceRecordSet.TTL", awsint64)
	if err != nil {
		return nil, err
	}
	err = setFieldWithType(params["value"], resourceRecord, "Value", awsstr)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	var output *route53.ChangeResourceRecordSetsOutput
	output, err = d.ChangeResourceRecordSets(input)

	if err != nil {
		return nil, fmt.Errorf("delete record: %s", err)
	}
	d.logger.ExtraVerbosef("route53.ChangeResourceRecordSets call took %s", time.Since(start))
	d.logger.Info("delete record done")
	return aws.StringValue(output.ChangeInfo.Id), nil
}

func buildIpPermissionsFromParams(params map[string]interface{}) ([]*ec2.IpPermission, error) {
	if _, ok := params["cidr"].(string); !ok {
		return nil, fmt.Errorf("invalid cidr '%v'", params["cidr"])
	}
	ipPerm := &ec2.IpPermission{
		IpRanges: []*ec2.IpRange{{CidrIp: aws.String(params["cidr"].(string))}},
	}
	if _, ok := params["protocol"].(string); !ok {
		return nil, fmt.Errorf("invalid protocol '%v'", params["protocol"])
	}
	p := params["protocol"].(string)
	if strings.Contains("any", p) {
		ipPerm.FromPort = aws.Int64(int64(-1))
		ipPerm.ToPort = aws.Int64(int64(-1))
		ipPerm.IpProtocol = aws.String("-1")
		return []*ec2.IpPermission{ipPerm}, nil
	}
	ipPerm.IpProtocol = aws.String(p)
	switch ports := params["portrange"].(type) {
	case int:
		ipPerm.FromPort = aws.Int64(int64(ports))
		ipPerm.ToPort = aws.Int64(int64(ports))
	case int64:
		ipPerm.FromPort = aws.Int64(ports)
		ipPerm.ToPort = aws.Int64(ports)
	case string:
		switch {
		case strings.Contains(ports, "any"):
			ipPerm.FromPort = aws.Int64(int64(-1))
			ipPerm.ToPort = aws.Int64(int64(-1))
		case strings.Contains(ports, "-"):
			from, err := strconv.ParseInt(strings.SplitN(ports, "-", 2)[0], 10, 64)
			if err != nil {
				return nil, err
			}
			to, err := strconv.ParseInt(strings.SplitN(ports, "-", 2)[1], 10, 64)
			if err != nil {
				return nil, err
			}
			ipPerm.FromPort = aws.Int64(from)
			ipPerm.ToPort = aws.Int64(to)
		default:
			port, err := strconv.ParseInt(ports, 10, 64)
			if err != nil {
				return nil, err
			}
			ipPerm.FromPort = aws.Int64(port)
			ipPerm.ToPort = aws.Int64(port)
		}
	}

	return []*ec2.IpPermission{ipPerm}, nil
}

func fakeDryRunId(entity string) string {
	suffix := rand.Intn(1e6)
	switch entity {
	case cloud.Instance:
		return fmt.Sprintf("i-%d", suffix)
	case cloud.Subnet:
		return fmt.Sprintf("subnet-%d", suffix)
	case cloud.Vpc:
		return fmt.Sprintf("vpc-%d", suffix)
	case cloud.Volume:
		return fmt.Sprintf("vol-%d", suffix)
	case cloud.SecurityGroup:
		return fmt.Sprintf("sg-%d", suffix)
	case cloud.InternetGateway:
		return fmt.Sprintf("igw-%d", suffix)
	default:
		return fmt.Sprintf("dryrunid-%d", suffix)
	}
}

type checker struct {
	description string
	timeout     time.Duration
	frequency   time.Duration
	fetchFunc   func() (string, error)
	expect      string
	logger      *logger.Logger
}

func (c *checker) check() error {
	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	for {
		select {
		case <-time.After(c.frequency):
			got, err := c.fetchFunc()
			if err != nil {
				return fmt.Errorf("check %s: %s", c.description, err)
			}
			if got == c.expect {
				c.logger.Infof("check %s status '%s' done", c.description, c.expect)
				return nil
			}
			c.logger.Infof("%s status '%s', expect '%s', retry in %s (timeout %s).", c.description, got, c.expect, c.frequency, c.timeout)
		case <-timer.C:
			return fmt.Errorf("timeout of %s expired", c.timeout)
		}
	}
}
