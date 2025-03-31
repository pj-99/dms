package main

import (
	"encoding/xml"
	"net/http"

	"github.com/anacrolix/dms/upnp" //TODO

	log "github.com/sirupsen/logrus"
)



var (
	status = false
	target = false

	switchPowerEventing *upnp.Eventing
)

type switchPowerService struct {
	upnp.Eventing
}

type setStatusReq struct {
	NewTargetValue bool `xml:"newTargetValue"`
}

func boolNum(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// For simplicity, only notify status chagned
func(s *switchPowerService) NotifyIfNeed(oldStatus bool) {
	curStatus := status
	log.Infof("Notify if need curstatus: %v, oldstatus: %v", curStatus, oldStatus)
	if oldStatus == curStatus {
		return
	}

	// TODO check
	// for sid, callback := range s.switchPowerEventing.subscriptions {
	// 	fmt.Printf("sid: %v, callback: %v", sid, callback)
	// 	notifyBody := fmt.Sprintf(`
	// 	<e:propertyset xmlns:e="urn:schemas-upnp-org:event-1-0">
	// 		<e:property>
	// 			<Status>%v</Status>
	// 		</e:property>
	// 	</e:propertyset>`, boolNum(curStatus))

	// 	notifyReq, _ := http.NewRequest("NOTIFY", callback, bytes.NewBuffer([]byte(notifyBody)))
    //     notifyReq.Header.Set("NT", "upnp:event")
    //     notifyReq.Header.Set("NTS", "upnp:propchange")
    //     notifyReq.Header.Set("SID", sid)
    //     notifyReq.Header.Set("SEQ", "0")
    //     notifyReq.Header.Set("Content-Type", "text/xml")
		
    //     resp, err := http.DefaultClient.Do(notifyReq)
    //     if err != nil {
    //         log.Println("Failed to send notify:", err)
    //     } else {
    //         log.Println("Notify sent to", callback, "with response code:", resp.StatusCode)
    //     }
	// }
}

func (s *switchPowerService) Handle(action string, argsXML []byte, r *http.Request) ([][2]string, error) {
	log.Printf("Received action!!! %s", action)
	// target := s.DeviceModel.GetTarget()
	// status := s.DeviceModel.GetStatus()
	// TODO: using %d to print boolean status
	switch action {
	case "GetStatus":
		log.Printf("GetStatus returning %v", boolNum(status))
		return [][2]string{
			{"ResultStatus", boolNum(status)},
		}, nil
	case "GetTarget":
		log.Printf("GetTarget returning %v", boolNum(target))
		return [][2]string{
			{"RetTargetValue", boolNum(target)},
		}, nil
	case "SetTarget":
		defer s.NotifyIfNeed(status)

		var setStatusReq setStatusReq
		if err := xml.Unmarshal([]byte(argsXML), &setStatusReq); err != nil {
			log.Warnf("Unmarshall failed in SetTarget: %v", err)
			return nil, err
		}
		log.Infof("Receive %v in Set target", setStatusReq)
		// s.DeviceModel.SetTarget(setStatusReq.NewTargetValue)
		target = setStatusReq.NewTargetValue

		return [][2]string{}, nil
	default:
		return nil, upnp.InvalidActionError
	}
}