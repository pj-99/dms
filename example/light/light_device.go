package main

import (
	_ "embed"
	"fmt"

	"github.com/anacrolix/dms/dlna/dms"
	"github.com/anacrolix/dms/server"
	"github.com/anacrolix/dms/upnp"
)



const (
	rootDeviceType              = "urn:schemas-upnp-org:service:BinaryLight:1"
	resPath                     = "/res"
	iconPath                    = "/icon"
	subtitlePath                = "/subtitle"
	rootDescPath                = "/rootDesc.xml"
	serviceControlURL           = "/ctl"
	deviceIconPath              = "/deviceIcon"
)



//go:embed "sonic.png"
var defaultIcon []byte

var (
	userAgentProduct= "chiikawa"
	friendlyName = "Mepu light"

	services = []*server.ServiceWithSCPD{
		{
			Service: upnp.Service{
				ServiceType: "urn:schemas-upnp-org:service:SwitchPower:1",
				ServiceId:   "urn:upnp-org:serviceId:SwitchPower:1",
				EventSubURL: "/evt/SwitchPower",
			},
			SCPD: switchPowerServiceDescription,
		},
	}

	lightDevice = &server.UpnpDevice{
		RootDeviceType:  "urn:schemas-upnp-org:service:BinaryLight:1",
		RootDeviceModelName:  fmt.Sprintf("%s %s", userAgentProduct, "1"),
		RootDeviceUUID: server.MakeDeviceUuid(friendlyName),
		ServiceList: services,
		DeviceIcons: []dms.Icon{
			{
				Width:    48,
				Height:   48,
				Depth:    8,
				Mimetype: "image/png",
				Bytes:   defaultIcon,
			},
			{
				Width:    128,
				Height:   128,
				Depth:    8,
				Mimetype: "image/png",
				Bytes:    defaultIcon,
			},
		},
	}
)

// //go:embed "icon.jpg"
// var defaultIcon []byte


// Service description for Binary Light service
var binaryLightServiceDescription = `<?xml version="1.0"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0">
	<specVersion>
		<major>1</major>
		<minor>0</minor>
	</specVersion>
	<actionList>
		<action>
			<name>SetTarget</name>
			<argumentList>
				<argument>
					<name>NewTargetValue</name>
					<direction>in</direction>
					<relatedStateVariable>Target</relatedStateVariable>
				</argument>
			</argumentList>
		</action>
		<action>
			<name>GetTarget</name>
			<argumentList>
				<argument>
					<name>CurrentTargetValue</name>
					<direction>out</direction>
					<relatedStateVariable>Target</relatedStateVariable>
				</argument>
			</argumentList>
		</action>
		<action>
			<name>GetStatus</name>
			<argumentList>
				<argument>
					<name>ResultStatus</name>
					<direction>out</direction>
					<relatedStateVariable>Status</relatedStateVariable>
				</argument>
			</argumentList>
		</action>
	</actionList>
	<serviceStateTable>
		<stateVariable sendEvents="no">
			<name>Target</name>
			<dataType>boolean</dataType>
		</stateVariable>
		<stateVariable sendEvents="yes">
			<name>Status</name>
			<dataType>boolean</dataType>
		</stateVariable>
	</serviceStateTable>
</scpd>`

