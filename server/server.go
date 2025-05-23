// Wrapper for the general UPnP server
// Supporting custom description and onAction handlers
package server

import (
	"bytes"
	"crypto/md5"
	_ "embed"
	"encoding/xml"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anacrolix/dms/dlna/dms"
	"github.com/anacrolix/dms/soap"
	"github.com/anacrolix/dms/ssdp"
	"github.com/anacrolix/dms/upnp"
	"github.com/anacrolix/log"
	"github.com/nfnt/resize"
)

const (
	serverVersion     = "1"
	serviceControlURL = "/ctl"
)

// An interface with these flags should be valid for SSDP.
const ssdpInterfaceFlags = net.FlagUp | net.FlagMulticast

var (
	serverField = fmt.Sprintf(`Linux/3.4 DLNADOC/1.50 UPnP/1.0 %s/%s`,
		"Testing-UPnP-Server",
		serverVersion)
	deviceIconPath = "/deviceIcon"
)

// Groups the ServiceWithSCPD definition with its XML description.
type ServiceWithSCPD struct {
	upnp.Service
	SCPD string
}

// default config
var config = &upnpConfig{
	Path:   "",
	IfName: "",
	Http:   ":1338",

	DeviceIcon: "",
	LogHeaders: false,
}

// The input device
type UpnpDevice struct {
	ServiceList         []*ServiceWithSCPD
	RootDeviceType      string
	RootDeviceModelName string
	RootDeviceUUID      string
	DeviceIcons         []dms.Icon
	FriendlyName        string
	Manufacturer        string

	Devices  []string
	Services []string

	UpnpServices map[string]upnp.UPnPService
}

type UpnpServer struct {
	HTTPConn       net.Listener
	Interfaces     []net.Interface
	httpServeMux   *http.ServeMux
	RootObjectPath string
	rootDescXML    []byte
	closed         chan struct{}
	ssdpStopped    chan struct{}
	// The service SOAP handler keyed by service URN.
	services map[string]upnp.UPnPService

	LogHeaders bool
	// Stall event subscription requests until they drop. A workaround for
	// some bad clients.
	StallEventSubscribe bool
	// Time interval between SSPD announces
	NotifyInterval time.Duration
	// White list of clients
	AllowedIpNets  []*net.IPNet
	Logger         log.Logger
	eventingLogger log.Logger

	DeviceDesc *upnp.DeviceDesc

	rootDescPath string

	UpnpDevice *UpnpDevice
}

type upnpConfig struct {
	Path                string
	IfName              string
	Http                string
	DeviceIcon          string
	LogHeaders          bool
	StallEventSubscribe bool
	NotifyInterval      time.Duration
	AllowedIpNets       []*net.IPNet
	DeviceIcons         []dms.Icon
}

func Start(d *UpnpDevice) error {
	path := flag.String("path", config.Path, "browse root path")
	ifName := flag.String("ifname", config.IfName, "specific SSDP network interface")
	http := flag.String("http", config.Http, "http server port")
	logHeaders := flag.Bool("logHeaders", config.LogHeaders, "log HTTP headers")
	allowedIps := flag.String("allowedIps", "", "allowed ip of clients, separated by comma")
	flag.BoolVar(&config.StallEventSubscribe, "stallEventSubscribe", false, "workaround for some bad event subscribers")
	flag.DurationVar(&config.NotifyInterval, "notifyInterval", 180*time.Second, "interval between SSPD announces")

	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		return fmt.Errorf("%s: %s\n", "unexpected positional arguments", flag.Args())
	}

	logger := log.Default.WithNames("main")

	config.Path, _ = filepath.Abs(*path)
	config.IfName = *ifName
	config.Http = *http
	config.LogHeaders = *logHeaders
	config.AllowedIpNets = makeIpNets(*allowedIps)

	logger.Printf("allowed ip nets are %q", config.AllowedIpNets)

	upnpServer := &UpnpServer{
		UpnpDevice: d,
		Logger:     logger.WithNames("dms", "server"),
		Interfaces: func(ifName string) (ifs []net.Interface) {
			var err error
			if ifName == "" {
				ifs, err = net.Interfaces()
			} else {
				var if_ *net.Interface
				if_, err = net.InterfaceByName(ifName)
				if if_ != nil {
					ifs = append(ifs, *if_)
				}
			}
			if err != nil {
				log.Fatal(err)
			}
			var tmp []net.Interface
			for _, if_ := range ifs {
				if if_.Flags&net.FlagUp == 0 || if_.MTU <= 0 {
					continue
				}
				tmp = append(tmp, if_)
			}
			ifs = tmp
			return
		}(config.IfName),
		HTTPConn: func() net.Listener {
			conn, err := net.Listen("tcp", config.Http)
			if err != nil {
				log.Fatal(err)
			}
			return conn
		}(),
		RootObjectPath:      filepath.Clean(config.Path),
		LogHeaders:          config.LogHeaders,
		StallEventSubscribe: config.StallEventSubscribe,
		NotifyInterval:      config.NotifyInterval,
		AllowedIpNets:       config.AllowedIpNets,
		rootDescPath:        "/rootDesc.xml",
	}
	if err := upnpServer.Init(); err != nil {
		log.Fatalf("error initing server: %v", err)
	}

	// Use goroutine run server
	go func() {
		if err := upnpServer.Run(); err != nil {
			log.Fatal(err)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)

	// Hang and wait until the sigs channel receive a signal
	<-sigs
	err := upnpServer.Close()
	if err != nil {
		log.Fatal(err)
	}
	return nil
}

func resizeImage(imageData image.Image, size uint) []byte {
	img := resize.Resize(size, size, imageData, resize.Lanczos3)
	var buff bytes.Buffer
	png.Encode(&buff, img)
	return buff.Bytes()
}

func readIcon(path string, size uint) []byte {
	r, err := getIconReader(path)
	if err != nil {
		panic(err)
	}
	defer r.Close()
	imageData, _, err := image.Decode(r)
	if err != nil {
		panic(err)
	}
	return resizeImage(imageData, size)
}

func getIconReader(path string) (io.ReadCloser, error) {
	if path == "" {
		fmt.Println("Icon path is empty")
		// return ioutil.NopCloser(bytes.NewReader(defaultIcon)), nil
	}
	return os.Open(path)
}

func (s *UpnpServer) InitDevice() {
	// Init SCPD
	for _, s := range s.UpnpDevice.ServiceList {
		lastInd := strings.LastIndex(s.ServiceId, ":")
		p := path.Join("/scpd", s.ServiceId[lastInd+1:])
		s.SCPDURL = p + ".xml"
	}

}

func (srv *UpnpServer) Init() (err error) {
	srv.InitDevice()

	srv.eventingLogger = srv.Logger.WithNames("eventing")
	srv.eventingLogger.Levelf(log.Debug, "hello %v", "world")

	srv.closed = make(chan struct{})
	if srv.HTTPConn == nil {
		srv.HTTPConn, err = net.Listen("tcp", "")
		if err != nil {
			log.Print(err)
			return
		}
	}
	if srv.Interfaces == nil {
		ifs, err := net.Interfaces()
		if err != nil {
			log.Print(err)
		}
		var tmp []net.Interface
		for _, if_ := range ifs {
			if if_.Flags&net.FlagUp == 0 || if_.MTU <= 0 {
				continue
			}
			tmp = append(tmp, if_)
		}
		srv.Interfaces = tmp
	}

	srv.httpServeMux = http.NewServeMux()
	// srv.rootDeviceUUID = MakeDeviceUuid(srv.FriendlyName)

	srv.rootDescXML, err = xml.MarshalIndent(
		upnp.DeviceDesc{
			SpecVersion: upnp.SpecVersion{Major: 1, Minor: 0},
			Device: upnp.Device{
				DeviceType: srv.UpnpDevice.RootDeviceType,
				// FriendlyName: srv.FriendlyName,
				FriendlyName: srv.UpnpDevice.FriendlyName,
				Manufacturer: srv.UpnpDevice.Manufacturer,
				ModelName:    srv.UpnpDevice.RootDeviceModelName,
				UDN:          srv.UpnpDevice.RootDeviceUUID,
				VendorXML:    ``,
				ServiceList: func() (ss []upnp.Service) {
					for _, s := range srv.UpnpDevice.ServiceList {
						ss = append(ss, s.Service)
					}
					return
				}(),
				IconList: func() (ret []upnp.Icon) {
					for i, di := range srv.UpnpDevice.DeviceIcons {
						ret = append(ret, upnp.Icon{
							Height:   di.Height,
							Width:    di.Width,
							Depth:    di.Depth,
							Mimetype: di.Mimetype,
							URL:      fmt.Sprintf("%s/%d", deviceIconPath, i),
						})
					}
					return
				}(),
				PresentationURL: "/",
			},
		},
		" ", "  ")
	if err != nil {
		return
	}
	srv.rootDescXML = append([]byte(`<?xml version="1.0"?>`), srv.rootDescXML...)
	srv.Logger.Println("HTTP srv on", srv.HTTPConn.Addr())
	srv.initMux(srv.httpServeMux)
	srv.ssdpStopped = make(chan struct{})
	return nil
}

func (srv *UpnpServer) Run() (err error) {
	go func() {
		srv.doSSDP()
		close(srv.ssdpStopped)
	}()
	return srv.serveHTTP()
}

func (s *UpnpServer) doSSDP() {
	var wg sync.WaitGroup
	for _, if_ := range s.Interfaces {
		if_ := if_
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.ssdpInterface(if_)
		}()
	}
	wg.Wait()
}

func (me *UpnpServer) httpPort() int {
	return me.HTTPConn.Addr().(*net.TCPAddr).Port
}

func (srv *UpnpServer) Close() (err error) {
	close(srv.closed)
	err = srv.HTTPConn.Close()
	<-srv.ssdpStopped
	return
}

// TODO: Document the use of this for debugging.
type mitmRespWriter struct {
	http.ResponseWriter
	loggedHeader bool
	logHeader    bool
}

func (me *UpnpServer) serveHTTP() error {
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if me.LogHeaders {
				fmt.Fprintf(os.Stderr, "%s %s\r\n", r.Method, r.RequestURI)
				r.Header.Write(os.Stderr)
				fmt.Fprintln(os.Stderr)
			}
			w.Header().Set("Ext", "")
			w.Header().Set("Server", serverField)
			me.httpServeMux.ServeHTTP(&mitmRespWriter{
				ResponseWriter: w,
				logHeader:      me.LogHeaders,
			}, r)
		}),
	}
	err := srv.Serve(me.HTTPConn)
	select {
	case <-me.closed:
		return nil
	default:
		return err
	}
}

func (me *UpnpServer) location(ip net.IP) string {
	url := url.URL{
		Scheme: "http",
		Host: (&net.TCPAddr{
			IP:   ip,
			Port: me.httpPort(),
		}).String(),
		Path: me.rootDescPath,
	}
	return url.String()
}

// Run SSDP server on an interface.
func (me *UpnpServer) ssdpInterface(if_ net.Interface) {
	logger := me.Logger.WithNames("ssdp", if_.Name)
	s := ssdp.Server{
		Interface: if_,
		// Devices:   devices(),
		Devices:  me.UpnpDevice.Devices,
		Services: me.UpnpDevice.Services,
		Location: func(ip net.IP) string {
			return me.location(ip)
		},
		Server:         serverField,
		UUID:           me.UpnpDevice.RootDeviceUUID,
		NotifyInterval: me.NotifyInterval,
		Logger:         logger,
	}
	if err := s.Init(); err != nil {
		if if_.Flags&ssdpInterfaceFlags != ssdpInterfaceFlags {
			// Didn't expect it to work anyway.
			return
		}
		if strings.Contains(err.Error(), "listen") {
			// OSX has a lot of dud interfaces. Failure to create a socket on
			// the interface are what we're expecting if the interface is no
			// good.
			return
		}
		logger.Printf("error creating ssdp server on %s: %s", if_.Name, err)
		return
	}
	defer s.Close()
	logger.Levelf(log.Info, "started SSDP on %q", if_.Name)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		if err := s.Serve(); err != nil {
			logger.Printf("%q: %q\n", if_.Name, err)
		}
	}()
	select {
	case <-me.closed:
		// Returning will close the server.
	case <-stopped:
	}
}

func (server *UpnpServer) initMux(mux *http.ServeMux) {
	// Handle root (presentationURL)
	// mux.HandleFunc("/", func(resp http.ResponseWriter, req *http.Request) {

	mux.HandleFunc(server.rootDescPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", `text/xml; charset="utf-8"`)
		w.Header().Set("content-length", fmt.Sprint(len(server.rootDescXML)))
		w.Header().Set("server", serverField)
		w.Write(server.rootDescXML)
	})
	server.handleSCPDs(mux)

	// Control
	mux.HandleFunc(serviceControlURL, server.serviceControlHandler)

	// Eventing
	for _, s := range server.UpnpDevice.ServiceList {
		serviceKey := getServiceKey(s.Service.ServiceType)

		mux.HandleFunc(s.Service.EventSubURL, func(service upnp.UPnPService) http.HandlerFunc {
			fmt.Println("service", service)
			return func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "SUBSCRIBE" {
					service.Subscribe(w, r)
				} else if r.Method == "UNSUBSCRIBE" {
					service.Unsubscribe(w, r)
				} else {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				}
			}
		}(server.UpnpDevice.UpnpServices[serviceKey]))
	}
	// mux.HandleFunc("/debug/pprof/", pprof.Index)
}

// Input: urn:schemas-upnp-org:service:SwitchPower:1
// Output: SwitchPower
func getServiceKey(serviceUrn string) string {
	parts := strings.Split(serviceUrn, ":")
	if len(parts) < 4 {
		panic("Invalid service URN")
	}
	return strings.Split(serviceUrn, ":")[3]
}

func xmlMarshalOrPanic(value interface{}) []byte {
	ret, err := xml.MarshalIndent(value, "", "  ")
	if err != nil {
		log.Panicf("xmlMarshalOrPanic failed to marshal %v: %s", value, err)
	}
	return ret
}

// Marshal SOAP response arguments into a response XML snippet.
func marshalSOAPResponse(sa upnp.SoapAction, args [][2]string) []byte {
	soapArgs := make([]soap.Arg, 0, len(args))
	for _, arg := range args {
		argName, value := arg[0], arg[1]
		soapArgs = append(soapArgs, soap.Arg{
			XMLName: xml.Name{Local: argName},
			Value:   value,
		})
	}
	return []byte(fmt.Sprintf(`<u:%[1]sResponse xmlns:u="%[2]s">%[3]s</u:%[1]sResponse>`, sa.Action, sa.ServiceURN.String(), xmlMarshalOrPanic(soapArgs)))
}

func (server *UpnpServer) serviceControlHandler(w http.ResponseWriter, r *http.Request) {
	// Handle a service control HTTP request.
	soapActionString := r.Header.Get("SOAPACTION")
	soapAction, err := upnp.ParseActionHTTPHeader(soapActionString)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var env soap.Envelope
	if err := xml.NewDecoder(r.Body).Decode(&env); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.Header().Set("Ext", "")
	w.Header().Set("Server", serverField)
	soapRespXML, code := func() ([]byte, int) {
		respArgs, err := server.soapActionResponse(soapAction, env.Body.Action, r)
		if err != nil {
			upnpErr := upnp.ConvertError(err)
			return xmlMarshalOrPanic(soap.NewFault("UPnPError", upnpErr)), 500
		}
		return marshalSOAPResponse(soapAction, respArgs), 200
	}()
	bodyStr := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8" standalone="yes"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>%s</s:Body></s:Envelope>`, soapRespXML)
	// Compatibility with Samsung Frame TV's - they don't display an empty content directory without this hack:
	bodyStr = strings.Replace(bodyStr, "&#34;", `"`, -1)
	w.WriteHeader(code)
	if _, err := w.Write([]byte(bodyStr)); err != nil {
		log.Print(err)
	}
}

func (server *UpnpServer) soapActionResponse(sa upnp.SoapAction, actionRequestXML []byte, r *http.Request) ([][2]string, error) {
	service, ok := server.UpnpDevice.UpnpServices[sa.Type]
	if !ok {
		return nil, upnp.Errorf(upnp.InvalidActionErrorCode, "Invalid service: %s", sa.Type)
	}

	// if service.needToNotify {
	// defer me.notify(service)
	// }
	return service.Handle(sa.Action, actionRequestXML, r)
}

func MakeDeviceUuid(unique string) string {
	h := md5.New()
	if _, err := io.WriteString(h, unique); err != nil {
		log.Panicf("makeDeviceUuid write failed: %s", err)
	}
	buf := h.Sum(nil)
	return upnp.FormatUUID(buf)
}

// Install handlers to serve SCPD for each UPnP service.
func (s *UpnpServer) handleSCPDs(mux *http.ServeMux) {

	// TODO?? what is this used for
	startTime := time.Now()

	for _, s := range s.UpnpDevice.ServiceList {
		mux.HandleFunc(s.SCPDURL, func(serviceDesc string) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", `text/xml; charset="utf-8"`)
				http.ServeContent(w, r, "", startTime, bytes.NewReader([]byte(serviceDesc)))
			}
		}(s.SCPD))
	}
}

func makeIpNets(s string) []*net.IPNet {
	var nets []*net.IPNet
	if len(s) < 1 {
		_, ipnet, _ := net.ParseCIDR("0.0.0.0/0")
		nets = append(nets, ipnet)
		_, ipnet, _ = net.ParseCIDR("::/0")
		nets = append(nets, ipnet)
	} else {
		for _, el := range strings.Split(s, ",") {
			ip := net.ParseIP(el)

			if ip == nil {
				_, ipnet, err := net.ParseCIDR(el)
				if err == nil {
					nets = append(nets, ipnet)
				} else {
					log.Printf("unable to parse expression %q", el)
				}

			} else {
				_, ipnet, err := net.ParseCIDR(el + "/32")
				if err == nil {
					nets = append(nets, ipnet)
				} else {
					log.Printf("unable to parse ip %q", el)
				}
			}
		}
	}
	return nets
}
