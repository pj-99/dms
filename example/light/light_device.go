package main

import (
	_ "embed"
	"fmt"

	"github.com/anacrolix/dms/dlna/dms"
	"github.com/anacrolix/dms/server"
	"github.com/anacrolix/dms/upnp"
)

var (
	userAgentProduct = "chiikawa"

	services = []*server.ServiceWithSCPD{
		{
			Service: upnp.Service{
				ServiceType: "urn:schemas-upnp-org:service:SwitchPower:1",
				ServiceId:   "urn:upnp-org:serviceId:SwitchPower:1",
				ControlURL:  "/ctl",
				EventSubURL: "/evt/SwitchPower",
			},
			SCPD: switchPowerServiceDescription,
		},
	}

	friendlyName = "Test mepu light"

	lightDevice = &server.UpnpDevice{
		FriendlyName:        friendlyName,
		Manufacturer:        "demo",
		RootDeviceType:      "urn:schemas-upnp-org:service:BinaryLight:1",
		RootDeviceModelName: fmt.Sprintf("%s %s", userAgentProduct, "1"),
		RootDeviceUUID:      server.MakeDeviceUuid(friendlyName),
		ServiceList:         services,
		DeviceIcons:         []dms.Icon{},
	}
)
