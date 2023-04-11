package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"

	"github.com/Fish-pro/learn-cni/pkg/nettool"
	"github.com/Fish-pro/learn-cni/pkg/util"
)

const filename = "/tmp/reserved_ips"

type PluginConf struct {
	types.NetConf
	RuntimeConfig *struct {
		TestConfig map[string]interface{} `json:"testConfig"`
	} `json:"runtimeConfig"`

	Bridge string `json:"bridge"`
	Subnet string `json:"subnet"`
	MTU    int    `json:"mtu"`
}

func init() {
	runtime.LockOSThread()
}

func cmdAdd(args *skel.CmdArgs) error {
	util.WriteLog("进入到 cmdAdd")
	util.WriteLog(
		"这里的 CmdArgs 是: ", "ContainerID: ", args.ContainerID,
		"NetNs: ", args.Netns,
		"IfName: ", args.IfName,
		"Args: ", args.Args,
		"Path: ", args.Path,
		"StdinData: ", string(args.StdinData))
	pluginConfig := &PluginConf{}
	if err := json.Unmarshal(args.StdinData, pluginConfig); err != nil {
		util.WriteLog("args.StdinData 转 pluginConfig 失败")
		return err
	}

	ips, err := nettool.GetAllIPs(pluginConfig.Subnet)
	if err != nil {
		return err
	}

	gwIP := ips[0]

	f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	// get all the reserved IPs from file
	content, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}
	reservedIPs := strings.Split(strings.TrimSpace(string(content)), "\n")

	podIP := ""
	for _, ip := range ips[1:] {
		reserved := false
		for _, rip := range reservedIPs {
			if ip == rip {
				reserved = true
				break
			}
		}
		if !reserved {
			podIP = ip
			reservedIPs = append(reservedIPs, podIP)
			break
		}
	}
	if podIP == "" {
		return fmt.Errorf("no IP available")
	}

	// Create or update bridge
	brName := pluginConfig.Bridge
	if brName != "" {
		// fall back to default bridge name: minicni0
		brName = "learncni"
	}

	mtu := pluginConfig.MTU
	if mtu == 0 {
		// fall back to default MTU: 1500
		mtu = 1500
	}

	br, err := nettool.CreateOrUpdateBridge(brName, gwIP, mtu)
	if err != nil {
		return err
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return err
	}

	if err := nettool.SetupVeth(netns, br, args.IfName, podIP, gwIP, mtu); err != nil {
		return err
	}

	// write reserved IPs back into file
	if err := os.WriteFile(filename, []byte(strings.Join(reservedIPs, "\n")), 0600); err != nil {
		return fmt.Errorf("failed to write reserved IPs into file: %v", err)
	}

	result := &current.Result{
		CNIVersion: pluginConfig.CNIVersion,
	}

	ipConfig := current.IPConfig{Gateway: net.ParseIP(gwIP), Address: net.IPNet{IP: net.ParseIP(podIP), Mask: net.CIDRMask(128, 128)}}

	result.IPs = append(result.IPs, &ipConfig)

	return types.PrintResult(result, pluginConfig.CNIVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	util.WriteLog("进入到 cmdDel")
	util.WriteLog(
		"这里的 CmdArgs 是: ", "ContainerID: ", args.ContainerID,
		"NetNs: ", args.Netns,
		"IfName: ", args.IfName,
		"Args: ", args.Args,
		"Path: ", args.Path,
		"StdinData: ", string(args.StdinData))
	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return err
	}
	ip, err := nettool.GetVethIPInNS(netns, args.IfName)
	if err != nil {
		return err
	}

	// open or create the file that stores all the reserved IPs
	f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("failed to open file that stores reserved IPs %v", err)
	}
	defer f.Close()

	// get all the reserved IPs from file
	content, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	reservedIPs := strings.Split(strings.TrimSpace(string(content)), "\n")

	for i, rip := range reservedIPs {
		if rip == ip {
			reservedIPs = append(reservedIPs[:i], reservedIPs[i+1:]...)
			break
		}
	}

	// write reserved IPs back into file
	if err := os.WriteFile(filename, []byte(strings.Join(reservedIPs, "\n")), 0600); err != nil {
		return fmt.Errorf("failed to write reserved IPs into file: %v", err)
	}

	return nil
}

func cmdCheck(args *skel.CmdArgs) error {
	util.WriteLog("进入到 cmdCheck")
	util.WriteLog(
		"这里的 CmdArgs 是: ", "ContainerID: ", args.ContainerID,
		"NetNs: ", args.Netns,
		"IfName: ", args.IfName,
		"Args: ", args.Args,
		"Path: ", args.Path,
		"StdinData: ", string(args.StdinData))
	return nil
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("learncni"))
}
