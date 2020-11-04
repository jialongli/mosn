package proxywasm

import (
	"context"
	"encoding/json"
	"sync"

	wasm "github.com/wasmerio/go-ext-wasm/wasmer"
	"mosn.io/api"
	"mosn.io/mosn/pkg/log"
	"mosn.io/pkg/buffer"
)

func init() {
	api.RegisterStream(ProxyWasm, CreateProxyWasmFilterFactory)
}

var ProxyWasm = "proxy-wasm"

var rootWasmInstance *wasm.Instance
var once sync.Once
var id int32

type StreamProxyWasmConfig struct {
	Path string `json:"path"`
}

type FilterConfigFactory struct {
	Config *StreamProxyWasmConfig
}

func (f *FilterConfigFactory) CreateFilterChain(context context.Context, callbacks api.StreamFilterChainFactoryCallbacks) {

	filter := NewFilter(context, f.Config)
	callbacks.AddStreamReceiverFilter(filter, api.BeforeRoute)
	callbacks.AddStreamSenderFilter(filter)

	if _, err := filter.wasm.Exports["proxy_on_context_create"](filter.id, root_id); err != nil {
		log.DefaultLogger.Errorf("wasm proxy_on_context_create err: %v", err)
	}
	filter.wasm.SetContextData(&wasmContext{filter, filter.wasm})
	log.DefaultLogger.Debugf("wasm filter init success")
}

func CreateProxyWasmFilterFactory(conf map[string]interface{}) (api.StreamFilterChainFactory, error) {
	log.DefaultLogger.Debugf("create proxy wasm stream filter factory")
	cfg, err := ParseStreamProxyWasmFilter(conf)
	if err != nil {
		return nil, err
	}

	initWasm(cfg.Path)
	rootWasmInstance = NewWasmInstance()

	if rootWasmInstance == nil {
		log.DefaultLogger.Errorf("wasm init error")
	}
	log.DefaultLogger.Debugf("wasm init %+v", rootWasmInstance)

	return &FilterConfigFactory{cfg}, nil
}

// ParseStreamPayloadLimitFilter
func ParseStreamProxyWasmFilter(cfg map[string]interface{}) (*StreamProxyWasmConfig, error) {
	filterConfig := &StreamProxyWasmConfig{}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, filterConfig); err != nil {
		return nil, err
	}
	return filterConfig, nil
}

// streamProxyWasmFilter is an implement of StreamReceiverFilter
type streamProxyWasmFilter struct {
	ctx      context.Context
	rhandler api.StreamReceiverFilterHandler
	shandler api.StreamSenderFilterHandler
	path     string
	wasm     *wasm.Instance
	once     bool
	id       int32
}

func NewFilter(ctx context.Context, wasm *StreamProxyWasmConfig) *streamProxyWasmFilter {
	if log.Proxy.GetLogLevel() >= log.DEBUG {
		log.DefaultLogger.Debugf("create a new proxy wasm filter")
	}
	id++
	return &streamProxyWasmFilter{
		ctx:  ctx,
		path: wasm.Path,
		once: true,
		id:   id,
		wasm: NewWasmInstance(),
	}
}

func (f *streamProxyWasmFilter) SetReceiveFilterHandler(handler api.StreamReceiverFilterHandler) {
	f.rhandler = handler
}

func (f *streamProxyWasmFilter) OnReceive(ctx context.Context, headers api.HeaderMap, buf buffer.IoBuffer, trailers api.HeaderMap) api.StreamFilterStatus {
	if log.Proxy.GetLogLevel() >= log.DEBUG {
		log.DefaultLogger.Debugf("proxy wasm stream do receive headers, id = %d", f.id)
	}
	if buf != nil && buf.Len() > 0 {
		if _, err := f.wasm.Exports["proxy_on_request_headers"](f.id, 0, 0); err != nil {
			log.DefaultLogger.Errorf("wasm proxy_on_request_headers err: %v", err)
		}
		if _, err := f.wasm.Exports["proxy_on_request_body"](f.id, buf.Len(), 1); err != nil {
			log.DefaultLogger.Errorf("wasm proxy_on_request_body err: %v", err)
		}
	} else {
		if _, err := f.wasm.Exports["proxy_on_request_headers"](f.id, 0, 1); err != nil {
			log.DefaultLogger.Errorf("wasm proxy_on_request_headers err: %v", err)
		}
	}

	return api.StreamFilterContinue
}

func (f *streamProxyWasmFilter) SetSenderFilterHandler(handler api.StreamSenderFilterHandler) {
	f.shandler = handler
}

func (f *streamProxyWasmFilter) Append(ctx context.Context, headers api.HeaderMap, buf buffer.IoBuffer, trailers api.HeaderMap) api.StreamFilterStatus {
	if log.Proxy.GetLogLevel() >= log.DEBUG {
		log.DefaultLogger.Debugf("proxy wasm stream do receive headers")
	}

	if _, err := f.wasm.Exports["proxy_on_response_headers"](f.id, 1, 0); err != nil {
		log.DefaultLogger.Errorf("wasm proxy_on_response_headers err: %v", err)
	}

	return api.StreamFilterContinue
}

func (f *streamProxyWasmFilter) OnDestroy() {
	if f.once {
		f.once = false
		f.wasm.Exports["proxy_on_log"](f.id)
		f.wasm.Exports["proxy_on_done"](f.id)
		f.wasm.Exports["proxy_on_delete"](f.id)
	}
}