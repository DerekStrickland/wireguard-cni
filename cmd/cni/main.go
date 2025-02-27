// Copyright 2017 CNI authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This is a sample chained plugin that supports multiple CNI versions. It
// parses prevResult according to the cniVersion
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
)

// PluginConf is whatever you expect your configuration json to be. This is whatever
// is passed in on stdin. Your plugin may wish to expose its functionality via
// runtime args, see CONVENTIONS.md in the CNI spec.
type PluginConf struct {
	// This embeds the standard NetConf structure which allows your plugin
	// to more easily parse standard fields like Name, Type, CNIVersion,
	// and PrevResult.
	types.NetConf

	RuntimeConfig *struct {
		SampleConfig map[string]interface{} `json:"sample"`
	} `json:"runtimeConfig"`

	// Add plugin-specifc flags here
	NodeManagerAddr string `json:"nodeManagerAddr"`
}

// parseConfig parses the supplied configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	conf := PluginConf{}

	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	// fmt.Fprintf(os.Stderr, "%s", stdin)
	// fmt.Fprintf(os.Stderr, "%#v", conf)
	// Parse previous result. This will parse, validate, and place the
	// previous result object into conf.PrevResult. If you need to modify
	// or inspect the PrevResult you will need to convert it to a concrete
	// versioned Result struct.
	if err := version.ParsePrevResult(&conf.NetConf); err != nil {
		return nil, fmt.Errorf("could not parse prevResult: %v", err)
	}
	// End previous result parsing

	// Do any validation here
	if conf.NodeManagerAddr != "" {
		conf.NodeManagerAddr = "http://localhost:5242"
	}

	return &conf, nil
}

// cmdAdd is called for ADD requests
func cmdAdd(args *skel.CmdArgs) error {
	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return err
	}

	// A plugin can be either an "originating" plugin or a "chained" plugin.
	// Originating plugins perform initial sandbox setup and do not require
	// any result from a previous plugin in the chain. A chained plugin
	// modifies sandbox configuration that was previously set up by an
	// originating plugin and may optionally require a PrevResult from
	// earlier plugins in the chain.

	// START chained plugin code
	if conf.PrevResult == nil {
		return fmt.Errorf("must be called as chained plugin")
	}

	// Convert the PrevResult to a concrete Result type that can be modified.
	prevResult, err := current.GetResult(conf.PrevResult)
	if err != nil {
		return fmt.Errorf("failed to convert prevResult: %v", err)
	}

	if len(prevResult.IPs) == 0 {
		return fmt.Errorf("got no container IPs")
	}

	// Pass the prevResult through this plugin to the next one
	result := prevResult

	// We're going to hard code this because I don't have time to really mess around with this

	// END chained plugin code

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return err
	}

	var device string
	ctx := context.Background()
	err = netns.Do(func(netns ns.NetNS) error {
		fmt.Fprintln(os.Stderr, "netns: ", args.Netns)
		fmt.Fprintln(os.Stderr, "netns name: ", filepath.Base(args.Netns))
		time.Sleep(1 * time.Second)
		device, err = addWgInterface(ctx, *conf, filepath.Base(args.Netns), result, netns)

		ip, iface, err := getResult(device, args.Netns)

		if err != nil {
			return fmt.Errorf("failed to get result %w", err)
		}

		fmt.Fprintln(os.Stderr, "IP: ", ip)
		fmt.Fprintln(os.Stderr, "Interface: ", iface)
		ip.Interface = ptr(0)
		ifaces := make([]*current.Interface, len(result.Interfaces)+1)
		ifaces[0] = &iface
		// append our interfaces to the new slice
		copy(ifaces[1:], result.Interfaces)
		result.Interfaces = ifaces
		result.IPs = append(result.IPs, &ip)

		return err
	})
	if err != nil {
		return fmt.Errorf("failed to add interface %w", err)
	}

	// START originating plugin code
	// if conf.PrevResult != nil {
	//	return fmt.Errorf("must be called as the first plugin")
	// }

	// Generate some fake container IPs and add to the result
	// result := &current.Result{CNIVersion: current.ImplementedSpecVersion}
	// result.Interfaces = []*current.Interface{
	// 	{
	// 		Name:    "intf0",
	// 		Sandbox: args.Netns,
	// 		Mac:     "00:11:22:33:44:55",
	// 	},
	// }
	// result.IPs = []*current.IPConfig{
	// 	{
	// 		Address:   "1.2.3.4/24",
	// 		Gateway:   "1.2.3.1",
	// 		// Interface is an index into the Interfaces array
	// 		// of the Interface element this IP applies to
	// 		Interface: current.Int(0),
	// 	}
	// }
	// END originating plugin code

	// Implement your plugin here

	// Pass through the result for the next plugin
	return types.PrintResult(result, conf.CNIVersion)
}

// cmdDel is called for DELETE requests
func cmdDel(args *skel.CmdArgs) error {
	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return err
	}
	_ = conf

	// Do your delete here

	return nil
}

func main() {
	// replace TODO with your plugin name
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("TODO"))
}

func cmdCheck(args *skel.CmdArgs) error {
	// TODO: implement
	return fmt.Errorf("not implemented")
}

func ptr[a any](val a) *a {
	return &val
}
