package monitor

import (
	"context"
	"fmt"
	"math/big"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/livepeer/go-livepeer/clog"

	"contrib.go.opencensus.io/exporter/prometheus"
	rprom "github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

type (
	SegmentUploadError    string
	SegmentTranscodeError string
)

const (
	SegmentUploadErrorUnknown               SegmentUploadError    = "Unknown"
	SegmentUploadErrorGenCreds              SegmentUploadError    = "GenCreds"
	SegmentUploadErrorOS                    SegmentUploadError    = "ObjectStorage"
	SegmentUploadErrorSessionEnded          SegmentUploadError    = "SessionEnded"
	SegmentUploadErrorInsufficientBalance   SegmentUploadError    = "InsufficientBalance"
	SegmentUploadErrorTimeout               SegmentUploadError    = "Timeout"
	SegmentUploadErrorDuplicateSegment      SegmentUploadError    = "DuplicateSegment"
	SegmentUploadErrorOrchestratorCapped    SegmentUploadError    = "OrchestratorCapped"
	SegmentTranscodeErrorUnknown            SegmentTranscodeError = "Unknown"
	SegmentTranscodeErrorUnknownResponse    SegmentTranscodeError = "UnknownResponse"
	SegmentTranscodeErrorTranscode          SegmentTranscodeError = "Transcode"
	SegmentTranscodeErrorOrchestratorBusy   SegmentTranscodeError = "OrchestratorBusy"
	SegmentTranscodeErrorOrchestratorCapped SegmentTranscodeError = "OrchestratorCapped"
	SegmentTranscodeErrorParseResponse      SegmentTranscodeError = "ParseResponse"
	SegmentTranscodeErrorReadBody           SegmentTranscodeError = "ReadBody"
	SegmentTranscodeErrorNoOrchestrators    SegmentTranscodeError = "NoOrchestrators"
	SegmentTranscodeErrorDownload           SegmentTranscodeError = "Download"
	SegmentTranscodeErrorSaveData           SegmentTranscodeError = "SaveData"
	SegmentTranscodeErrorSessionEnded       SegmentTranscodeError = "SessionEnded"
	SegmentTranscodeErrorDuplicateSegment   SegmentTranscodeError = "DuplicateSegment"

	numberOfSegmentsToCalcAverage = 30
	gweiConversionFactor          = 1000000000

	logLevel = 6 // TODO move log levels definitions to separate package
	// importing `common` package here introduces import cycles
)

type NodeType string

const (
	Default      NodeType = "dflt"
	Orchestrator NodeType = "orch"
	Broadcaster  NodeType = "bctr"
	Transcoder   NodeType = "trcr"
	Redeemer     NodeType = "rdmr"

	segTypeRegular = "regular"
	segTypeRec     = "recorded" // segment in the stream for which recording is enabled
)

// Enabled true if metrics was enabled in command line
var Enabled bool
var PerStreamMetrics bool

// ExposeClientIP if true then Orchestrator exposes Broadcaster's IP address in metrics
var ExposeClientIP bool

var NodeID string

var timeToWaitForError = 8500 * time.Millisecond
var timeoutWatcherPause = 15 * time.Second

type (
	CensusMetricsCounter struct {
		nodeType                      NodeType
		nodeID                        string
		ctx                           context.Context
		kGPU                          tag.Key
		kNodeType                     tag.Key
		kNodeID                       tag.Key
		kProfile                      tag.Key
		kProfiles                     tag.Key
		kErrorCode                    tag.Key
		kTry                          tag.Key
		kSender                       tag.Key
		kRecipient                    tag.Key
		kManifestID                   tag.Key
		kSegmentType                  tag.Key
		kTrusted                      tag.Key
		kVerified                     tag.Key
		kClientIP                     tag.Key
		kOrchestratorURI              tag.Key
		mSegmentSourceAppeared        *stats.Int64Measure
		mSegmentEmerged               *stats.Int64Measure
		mSegmentEmergedUnprocessed    *stats.Int64Measure
		mSegmentUploaded              *stats.Int64Measure
		mSegmentUploadFailed          *stats.Int64Measure
		mSegmentDownloaded            *stats.Int64Measure
		mSegmentTranscoded            *stats.Int64Measure
		mSegmentTranscodedUnprocessed *stats.Int64Measure
		mSegmentTranscodeFailed       *stats.Int64Measure
		mSegmentTranscodedAppeared    *stats.Int64Measure
		mSegmentTranscodedAllAppeared *stats.Int64Measure
		mStartBroadcastClientFailed   *stats.Int64Measure
		mStreamCreateFailed           *stats.Int64Measure
		mStreamCreated                *stats.Int64Measure
		mStreamStarted                *stats.Int64Measure
		mStreamEnded                  *stats.Int64Measure
		mMaxSessions                  *stats.Int64Measure
		mCurrentSessions              *stats.Int64Measure
		mDiscoveryError               *stats.Int64Measure
		mTranscodeRetried             *stats.Int64Measure
		mTranscodersNumber            *stats.Int64Measure
		mTranscodersCapacity          *stats.Int64Measure
		mTranscodersLoad              *stats.Int64Measure
		mSuccessRate                  *stats.Float64Measure
		mSuccessRatePerStream         *stats.Float64Measure
		mTranscodeTime                *stats.Float64Measure
		mTranscodeLatency             *stats.Float64Measure
		mTranscodeOverallLatency      *stats.Float64Measure
		mUploadTime                   *stats.Float64Measure
		mDownloadTime                 *stats.Float64Measure
		mAuthWebhookTime              *stats.Float64Measure
		mSourceSegmentDuration        *stats.Float64Measure
		mHTTPClientTimeout1           *stats.Int64Measure
		mHTTPClientTimeout2           *stats.Int64Measure
		mRealtimeRatio                *stats.Float64Measure
		mRealtime3x                   *stats.Int64Measure
		mRealtime2x                   *stats.Int64Measure
		mRealtime1x                   *stats.Int64Measure
		mRealtimeHalf                 *stats.Int64Measure
		mRealtimeSlow                 *stats.Int64Measure
		mTranscodeScore               *stats.Float64Measure
		mRecordingSaveLatency         *stats.Float64Measure
		mRecordingSaveErrors          *stats.Int64Measure
		mRecordingSavedSegments       *stats.Int64Measure
		mOrchestratorSwaps            *stats.Int64Measure

		// Metrics for sending payments
		mTicketValueSent     *stats.Float64Measure
		mTicketsSent         *stats.Int64Measure
		mPaymentCreateError  *stats.Int64Measure
		mDeposit             *stats.Float64Measure
		mReserve             *stats.Float64Measure
		mMaxTranscodingPrice *stats.Float64Measure
		// Metrics for receiving payments
		mTicketValueRecv       *stats.Float64Measure
		mTicketsRecv           *stats.Int64Measure
		mPaymentRecvErr        *stats.Int64Measure
		mWinningTicketsRecv    *stats.Int64Measure
		mValueRedeemed         *stats.Float64Measure
		mTicketRedemptionError *stats.Int64Measure
		mSuggestedGasPrice     *stats.Float64Measure
		mMinGasPrice           *stats.Float64Measure
		mMaxGasPrice           *stats.Float64Measure
		mTranscodingPrice      *stats.Float64Measure

		// Metrics for pixel accounting
		mMilPixelsProcessed *stats.Float64Measure

		// Metrics for fast verification
		mFastVerificationDone                   *stats.Int64Measure
		mFastVerificationFailed                 *stats.Int64Measure
		mFastVerificationEnabledCurrentSessions *stats.Int64Measure
		mFastVerificationUsingCurrentSessions   *stats.Int64Measure

		lock        sync.Mutex
		emergeTimes map[uint64]map[uint64]time.Time // nonce:seqNo
		success     map[uint64]*segmentsAverager
	}

	segmentCount struct {
		seqNo       uint64
		emergedTime time.Time
		emerged     int
		transcoded  int
		failed      bool
	}

	tryData struct {
		first time.Time
		tries int
	}

	segmentsAverager struct {
		manifestID string
		segments   []segmentCount
		start      int
		end        int
		removed    bool
		removedAt  time.Time
		tries      map[uint64]tryData // seqNo:try
	}
)

// Exporter Prometheus exporter that handles `/metrics` endpoint
var Exporter *prometheus.Exporter

var census CensusMetricsCounter
var GlobalViews []*view.View

// used in unit tests
var unitTestMode bool

func InitCensus(nodeType NodeType, version string) {
	census = CensusMetricsCounter{
		emergeTimes: make(map[uint64]map[uint64]time.Time),
		nodeID:      NodeID,
		nodeType:    nodeType,
		success:     make(map[uint64]*segmentsAverager),
	}
	var err error
	ctx := context.Background()
	census.kGPU = tag.MustNewKey("gpu")
	census.kNodeType = tag.MustNewKey("node_type")
	census.kNodeID = tag.MustNewKey("node_id")
	census.kProfile = tag.MustNewKey("profile")
	census.kProfiles = tag.MustNewKey("profiles")
	census.kErrorCode = tag.MustNewKey("error_code")
	census.kTry = tag.MustNewKey("try")
	census.kSender = tag.MustNewKey("sender")
	census.kRecipient = tag.MustNewKey("recipient")
	census.kManifestID = tag.MustNewKey("manifest_id")
	census.kSegmentType = tag.MustNewKey("seg_type")
	census.kTrusted = tag.MustNewKey("trusted")
	census.kVerified = tag.MustNewKey("verified")
	census.kClientIP = tag.MustNewKey("client_ip")
	census.kOrchestratorURI = tag.MustNewKey("orchestrator_uri")
	census.ctx, err = tag.New(ctx, tag.Insert(census.kNodeType, string(nodeType)), tag.Insert(census.kNodeID, NodeID))
	if err != nil {
		glog.Fatal("Error creating context", err)
	}
	census.mHTTPClientTimeout1 = stats.Int64("http_client_timeout_1", "Number of times HTTP connection was dropped before transcoding complete", "tot")
	census.mHTTPClientTimeout2 = stats.Int64("http_client_timeout_2", "Number of times HTTP connection was dropped before transcoded segments was sent back to client", "tot")
	census.mRealtimeRatio = stats.Float64("http_client_segment_transcoded_realtime_ratio", "Ratio of source segment duration / transcode time as measured on HTTP client", "rat")
	census.mRealtime3x = stats.Int64("http_client_segment_transcoded_realtime_3x", "Number of segment transcoded 3x faster than realtime", "tot")
	census.mRealtime2x = stats.Int64("http_client_segment_transcoded_realtime_2x", "Number of segment transcoded 2x faster than realtime", "tot")
	census.mRealtime1x = stats.Int64("http_client_segment_transcoded_realtime_1x", "Number of segment transcoded 1x faster than realtime", "tot")
	census.mRealtimeHalf = stats.Int64("http_client_segment_transcoded_realtime_half", "Number of segment transcoded no more than two times slower than realtime", "tot")
	census.mRealtimeSlow = stats.Int64("http_client_segment_transcoded_realtime_slow", "Number of segment transcoded more than two times slower than realtime", "tot")
	census.mSegmentSourceAppeared = stats.Int64("segment_source_appeared_total", "SegmentSourceAppeared", "tot")
	census.mSegmentEmerged = stats.Int64("segment_source_emerged_total", "SegmentEmerged", "tot")
	census.mSegmentEmergedUnprocessed = stats.Int64("segment_source_emerged_unprocessed_total", "SegmentEmerged, counted by number of transcode profiles", "tot")
	census.mSegmentUploaded = stats.Int64("segment_source_uploaded_total", "SegmentUploaded", "tot")
	census.mSegmentUploadFailed = stats.Int64("segment_source_upload_failed_total", "SegmentUploadedFailed", "tot")
	census.mSegmentDownloaded = stats.Int64("segment_transcoded_downloaded_total", "SegmentDownloaded", "tot")
	census.mSegmentTranscoded = stats.Int64("segment_transcoded_total", "SegmentTranscoded", "tot")
	census.mSegmentTranscodedUnprocessed = stats.Int64("segment_transcoded_unprocessed_total", "SegmentTranscodedUnprocessed", "tot")
	census.mSegmentTranscodeFailed = stats.Int64("segment_transcode_failed_total", "SegmentTranscodeFailed", "tot")
	census.mSegmentTranscodedAppeared = stats.Int64("segment_transcoded_appeared_total", "SegmentTranscodedAppeared", "tot")
	census.mSegmentTranscodedAllAppeared = stats.Int64("segment_transcoded_all_appeared_total", "SegmentTranscodedAllAppeared", "tot")
	census.mStartBroadcastClientFailed = stats.Int64("broadcast_client_start_failed_total", "StartBroadcastClientFailed", "tot")
	census.mStreamCreateFailed = stats.Int64("stream_create_failed_total", "StreamCreateFailed", "tot")
	census.mStreamCreated = stats.Int64("stream_created_total", "StreamCreated", "tot")
	census.mStreamStarted = stats.Int64("stream_started_total", "StreamStarted", "tot")
	census.mStreamEnded = stats.Int64("stream_ended_total", "StreamEnded", "tot")
	census.mMaxSessions = stats.Int64("max_sessions_total", "MaxSessions", "tot")
	census.mCurrentSessions = stats.Int64("current_sessions_total", "Number of currently transcoded streams", "tot")
	census.mDiscoveryError = stats.Int64("discovery_errors_total", "Number of discover errors", "tot")
	census.mTranscodeRetried = stats.Int64("transcode_retried", "Number of times segment transcode was retried", "tot")
	census.mTranscodersNumber = stats.Int64("transcoders_number", "Number of transcoders currently connected to orchestrator", "tot")
	census.mTranscodersCapacity = stats.Int64("transcoders_capacity", "Total advertised capacity of transcoders currently connected to orchestrator", "tot")
	census.mTranscodersLoad = stats.Int64("transcoders_load", "Total load of transcoders currently connected to orchestrator", "tot")
	census.mSuccessRate = stats.Float64("success_rate", "Success rate", "per")
	census.mSuccessRatePerStream = stats.Float64("success_rate_per_stream", "Success rate, per stream", "per")
	census.mTranscodeTime = stats.Float64("transcode_time_seconds", "Transcoding time", "sec")
	census.mTranscodeLatency = stats.Float64("transcode_latency_seconds",
		"Transcoding latency, from source segment emerged from segmenter till transcoded segment apeeared in manifest", "sec")
	census.mTranscodeOverallLatency = stats.Float64("transcode_overall_latency_seconds",
		"Transcoding latency, from source segment emerged from segmenter till all transcoded segment apeeared in manifest", "sec")
	census.mUploadTime = stats.Float64("upload_time_seconds", "Upload (to Orchestrator) time", "sec")
	census.mDownloadTime = stats.Float64("download_time_seconds", "Download (from orchestrator) time", "sec")
	census.mAuthWebhookTime = stats.Float64("auth_webhook_time_milliseconds", "Authentication webhook execution time", "ms")
	census.mSourceSegmentDuration = stats.Float64("source_segment_duration_seconds", "Source segment's duration", "sec")
	census.mTranscodeScore = stats.Float64("transcode_score", "Ratio of source segment duration vs. transcode time", "rat")
	census.mRecordingSaveLatency = stats.Float64("recording_save_latency",
		"How long it takes to save segment to the OS", "sec")
	census.mRecordingSaveErrors = stats.Int64("recording_save_errors", "Number of errors during save to the recording OS", "tot")
	census.mRecordingSavedSegments = stats.Int64("recording_saved_segments", "Number of segments saved to the recording OS", "tot")
	census.mOrchestratorSwaps = stats.Int64("orchestrator_swaps", "Number of orchestrator swaps mid-stream", "tot")

	// Metrics for sending payments
	census.mTicketValueSent = stats.Float64("ticket_value_sent", "TicketValueSent", "gwei")
	census.mTicketsSent = stats.Int64("tickets_sent", "TicketsSent", "tot")
	census.mPaymentCreateError = stats.Int64("payment_create_errors", "PaymentCreateError", "tot")
	census.mDeposit = stats.Float64("broadcaster_deposit", "Current remaining deposit for the broadcaster node", "gwei")
	census.mReserve = stats.Float64("broadcaster_reserve", "Current remaing reserve for the broadcaster node", "gwei")
	census.mMaxTranscodingPrice = stats.Float64("max_transcoding_price", "MaxTranscodingPrice", "wei")

	// Metrics for receiving payments
	census.mTicketValueRecv = stats.Float64("ticket_value_recv", "TicketValueRecv", "gwei")
	census.mTicketsRecv = stats.Int64("tickets_recv", "TicketsRecv", "tot")
	census.mPaymentRecvErr = stats.Int64("payment_recv_errors", "PaymentRecvErr", "tot")
	census.mWinningTicketsRecv = stats.Int64("winning_tickets_recv", "WinningTicketsRecv", "tot")
	census.mValueRedeemed = stats.Float64("value_redeemed", "ValueRedeemed", "gwei")
	census.mTicketRedemptionError = stats.Int64("ticket_redemption_errors", "TicketRedemptionError", "tot")
	census.mSuggestedGasPrice = stats.Float64("suggested_gas_price", "SuggestedGasPrice", "gwei")
	census.mMinGasPrice = stats.Float64("min_gas_price", "MinGasPrice", "gwei")
	census.mMaxGasPrice = stats.Float64("max_gas_price", "MaxGasPrice", "gwei")
	census.mTranscodingPrice = stats.Float64("transcoding_price", "TranscodingPrice", "wei")

	// Metrics for pixel accounting
	census.mMilPixelsProcessed = stats.Float64("mil_pixels_processed", "MilPixelsProcessed", "mil pixels")

	// Metrics for fast verification
	census.mFastVerificationDone = stats.Int64("fast_verification_done", "FastVerificationDone", "tot")
	census.mFastVerificationFailed = stats.Int64("fast_verification_failed", "FastVerificationFailed", "tot")
	census.mFastVerificationEnabledCurrentSessions = stats.Int64("fast_verification_enabled_current_sessions_total",
		"Number of currently transcoded streams that have fast verification enabled", "tot")
	census.mFastVerificationUsingCurrentSessions = stats.Int64("fast_verification_using_current_sessions_total",
		"Number of currently transcoded streams that have fast verification enabled and that are using an untrusted orchestrator", "tot")

	glog.Infof("Compiler: %s Arch %s OS %s Go version %s", runtime.Compiler, runtime.GOARCH, runtime.GOOS, runtime.Version())
	glog.Infof("Livepeer version: %s", version)
	glog.Infof("Node type %s node ID %s", nodeType, NodeID)
	mVersions := stats.Int64("versions", "Version information.", "Num")
	compiler := tag.MustNewKey("compiler")
	goarch := tag.MustNewKey("goarch")
	goos := tag.MustNewKey("goos")
	goversion := tag.MustNewKey("goversion")
	livepeerversion := tag.MustNewKey("livepeerversion")
	ctx, err = tag.New(ctx, tag.Insert(census.kNodeType, string(nodeType)), tag.Insert(census.kNodeID, NodeID),
		tag.Insert(compiler, runtime.Compiler), tag.Insert(goarch, runtime.GOARCH), tag.Insert(goos, runtime.GOOS),
		tag.Insert(goversion, runtime.Version()), tag.Insert(livepeerversion, version))
	if err != nil {
		glog.Fatal("Error creating tagged context", err)
	}
	baseTags := []tag.Key{census.kNodeID, census.kNodeType}
	baseTagsWithManifestID := baseTags
	baseTagsWithEthAddr := baseTags
	baseTagsWithManifestIDAndEthAddr := baseTags
	if PerStreamMetrics {
		baseTagsWithManifestID = []tag.Key{census.kNodeID, census.kNodeType, census.kManifestID}
		baseTagsWithEthAddr = []tag.Key{census.kNodeID, census.kNodeType, census.kSender}
		baseTagsWithManifestIDAndEthAddr = []tag.Key{census.kNodeID, census.kNodeType, census.kManifestID, census.kSender}
	}
	baseTagsWithManifestIDAndIP := baseTagsWithManifestID
	if ExposeClientIP {
		baseTagsWithManifestIDAndIP = append([]tag.Key{census.kClientIP}, baseTagsWithManifestID...)
	}

	views := []*view.View{
		{
			Name:        "versions",
			Measure:     mVersions,
			Description: "Versions used by LivePeer node.",
			TagKeys:     []tag.Key{census.kNodeType, compiler, goos, goversion, livepeerversion},
			Aggregation: view.LastValue(),
		},
		{
			Name:        "broadcast_client_start_failed_total",
			Measure:     census.mStartBroadcastClientFailed,
			Description: "StartBroadcastClientFailed",
			TagKeys:     baseTags,
			Aggregation: view.Count(),
		},
		{
			Name:        "stream_created_total",
			Measure:     census.mStreamCreated,
			Description: "StreamCreated",
			TagKeys:     baseTags,
			Aggregation: view.Count(),
		},
		{
			Name:        "stream_started_total",
			Measure:     census.mStreamStarted,
			Description: "StreamStarted",
			TagKeys:     baseTags,
			Aggregation: view.Count(),
		},
		{
			Name:        "stream_ended_total",
			Measure:     census.mStreamEnded,
			Description: "StreamEnded",
			TagKeys:     baseTags,
			Aggregation: view.Count(),
		},
		{
			Name:        "stream_create_failed_total",
			Measure:     census.mStreamCreateFailed,
			Description: "StreamCreateFailed",
			TagKeys:     baseTags,
			Aggregation: view.Count(),
		},
		{
			Name:        "http_client_timeout_1",
			Measure:     census.mHTTPClientTimeout1,
			Description: "Number of times HTTP connection was dropped before transcoding complete",
			TagKeys:     baseTagsWithManifestIDAndIP,
			Aggregation: view.Count(),
		},
		{
			Name:        "http_client_timeout_2",
			Measure:     census.mHTTPClientTimeout2,
			Description: "Number of times HTTP connection was dropped before transcoded segments was sent back to client",
			TagKeys:     baseTagsWithManifestIDAndIP,
			Aggregation: view.Count(),
		},
		{
			Name:        "http_client_segment_transcoded_realtime_ratio",
			Measure:     census.mRealtimeRatio,
			Description: "Ratio of source segment duration / transcode time as measured on HTTP client",
			TagKeys:     baseTagsWithManifestIDAndIP,
			Aggregation: view.Distribution(0.5, 1, 2, 3, 5, 10, 50, 100),
		},
		{
			Name:        "http_client_segment_transcoded_realtime_3x",
			Measure:     census.mRealtime3x,
			Description: "Number of segment transcoded 3x faster than realtime",
			TagKeys:     baseTagsWithManifestIDAndIP,
			Aggregation: view.Count(),
		},
		{
			Name:        "http_client_segment_transcoded_realtime_2x",
			Measure:     census.mRealtime2x,
			Description: "Number of segment transcoded 2x faster than realtime",
			TagKeys:     baseTagsWithManifestIDAndIP,
			Aggregation: view.Count(),
		},
		{
			Name:        "http_client_segment_transcoded_realtime_1x",
			Measure:     census.mRealtime1x,
			Description: "Number of segment transcoded 1x faster than realtime",
			TagKeys:     baseTagsWithManifestIDAndIP,
			Aggregation: view.Count(),
		},
		{
			Name:        "http_client_segment_transcoded_realtime_half",
			Measure:     census.mRealtimeHalf,
			Description: "Number of segment transcoded no more than two times slower than realtime",
			TagKeys:     baseTagsWithManifestIDAndIP,
			Aggregation: view.Count(),
		},
		{
			Name:        "http_client_segment_transcoded_realtime_slow",
			Measure:     census.mRealtimeSlow,
			Description: "Number of segment transcoded more than two times slower than realtime",
			TagKeys:     baseTagsWithManifestIDAndIP,
			Aggregation: view.Count(),
		},
		{
			Name:        "segment_source_appeared_total",
			Measure:     census.mSegmentSourceAppeared,
			Description: "SegmentSourceAppeared",
			TagKeys:     append([]tag.Key{census.kProfile, census.kSegmentType}, baseTagsWithManifestID...),
			Aggregation: view.Count(),
		},
		{
			Name:        "segment_source_emerged_total",
			Measure:     census.mSegmentEmerged,
			Description: "SegmentEmerged",
			TagKeys:     baseTagsWithManifestID,
			Aggregation: view.Count(),
		},
		{
			Name:        "segment_source_emerged_unprocessed_total",
			Measure:     census.mSegmentEmergedUnprocessed,
			Description: "Raw number of segments emerged from segmenter.",
			TagKeys:     baseTagsWithManifestIDAndIP,
			Aggregation: view.Count(),
		},
		{
			Name:        "segment_source_uploaded_total",
			Measure:     census.mSegmentUploaded,
			Description: "SegmentUploaded",
			TagKeys:     append([]tag.Key{census.kOrchestratorURI}, baseTagsWithManifestID...),
			Aggregation: view.Count(),
		},
		{
			Name:        "segment_source_upload_failed_total",
			Measure:     census.mSegmentUploadFailed,
			Description: "SegmentUploadedFailed",
			TagKeys:     append([]tag.Key{census.kErrorCode, census.kOrchestratorURI}, baseTagsWithManifestID...),
			Aggregation: view.Count(),
		},
		{
			Name:        "segment_transcoded_downloaded_total",
			Measure:     census.mSegmentDownloaded,
			Description: "SegmentDownloaded",
			TagKeys:     baseTagsWithManifestID,
			Aggregation: view.Count(),
		},
		{
			Name:        "segment_transcoded_total",
			Measure:     census.mSegmentTranscoded,
			Description: "SegmentTranscoded",
			TagKeys:     append([]tag.Key{census.kProfiles, census.kTrusted, census.kVerified}, baseTags...),
			Aggregation: view.Count(),
		},
		{
			Name:        "segment_transcoded_unprocessed_total",
			Measure:     census.mSegmentTranscodedUnprocessed,
			Description: "Raw number of segments successfully transcoded.",
			TagKeys:     append([]tag.Key{census.kProfiles}, baseTagsWithManifestID...),
			Aggregation: view.Count(),
		},
		{
			Name:        "segment_transcode_failed_total",
			Measure:     census.mSegmentTranscodeFailed,
			Description: "SegmentTranscodeFailed",
			TagKeys:     append([]tag.Key{census.kErrorCode}, baseTagsWithManifestID...),
			Aggregation: view.Count(),
		},
		{
			Name:        "segment_transcoded_appeared_total",
			Measure:     census.mSegmentTranscodedAppeared,
			Description: "SegmentTranscodedAppeared",
			TagKeys:     append([]tag.Key{census.kProfile, census.kSegmentType}, baseTagsWithManifestID...),
			Aggregation: view.Count(),
		},
		{
			Name:        "segment_transcoded_all_appeared_total",
			Measure:     census.mSegmentTranscodedAllAppeared,
			Description: "SegmentTranscodedAllAppeared",
			TagKeys:     append([]tag.Key{census.kProfiles}, baseTagsWithManifestID...),
			Aggregation: view.Count(),
		},
		{
			Name:        "success_rate",
			Measure:     census.mSuccessRate,
			Description: "Number of transcoded segments divided on number of source segments",
			TagKeys:     baseTags,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "success_rate_per_stream",
			Measure:     census.mSuccessRatePerStream,
			Description: "Number of transcoded segments divided on number of source segments, per stream",
			TagKeys:     baseTagsWithManifestID,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "transcode_time_seconds",
			Measure:     census.mTranscodeTime,
			Description: "TranscodeTime, seconds",
			TagKeys:     append([]tag.Key{census.kProfiles, census.kTrusted, census.kVerified}, baseTags...),
			Aggregation: view.Distribution(0, .250, .500, .750, 1.000, 1.250, 1.500, 2.000, 2.500, 3.000, 3.500, 4.000, 4.500, 5.000, 10.000),
		},
		{
			Name:        "transcode_latency_seconds",
			Measure:     census.mTranscodeLatency,
			Description: "Transcoding latency, from source segment emerged from segmenter till transcoded segment apeeared in manifest",
			TagKeys:     append([]tag.Key{census.kProfile}, baseTagsWithManifestID...),
			Aggregation: view.Distribution(0, .500, .75, 1.000, 1.500, 2.000, 2.500, 3.000, 3.500, 4.000, 4.500, 5.000, 10.000),
		},
		{
			Name:        "transcode_overall_latency_seconds",
			Measure:     census.mTranscodeOverallLatency,
			Description: "Transcoding latency, from source segment emerged from segmenter till all transcoded segment apeeared in manifest",
			TagKeys:     append([]tag.Key{census.kProfiles}, baseTagsWithManifestID...),
			Aggregation: view.Distribution(0, .500, .75, 1.000, 1.500, 2.000, 2.500, 3.000, 3.500, 4.000, 4.500, 5.000, 10.000),
		},
		{
			Name:        "transcode_score",
			Measure:     census.mTranscodeScore,
			Description: "Ratio of source segment duration vs. transcode time",
			TagKeys:     append([]tag.Key{census.kProfiles, census.kTrusted, census.kVerified}, baseTags...),
			Aggregation: view.Distribution(0, .5, 1, 1.5, 2, 2.5, 3, 3.5, 4, 4.5, 5, 10, 15, 20, 40),
		},
		{
			Name:        "recording_save_latency",
			Measure:     census.mRecordingSaveLatency,
			Description: "How long it takes to save segment to the OS",
			TagKeys:     baseTags,
			Aggregation: view.Distribution(0, .500, .75, 1.000, 1.500, 2.000, 2.500, 3.000, 3.500, 4.000, 4.500, 5.000, 10.000, 30.000),
		},
		{
			Name:        "recording_save_errors",
			Measure:     census.mRecordingSaveErrors,
			Description: "Number of errors during save to the recording OS",
			TagKeys:     baseTags,
			Aggregation: view.Count(),
		},
		{
			Name:        "recording_saved_segments",
			Measure:     census.mRecordingSavedSegments,
			Description: "Number of segments saved to the recording OS",
			TagKeys:     baseTags,
			Aggregation: view.Count(),
		},
		{
			Name:        "upload_time_seconds",
			Measure:     census.mUploadTime,
			Description: "UploadTime, seconds",
			TagKeys:     append([]tag.Key{census.kOrchestratorURI}, baseTags...),
			Aggregation: view.Distribution(0, .10, .20, .50, .100, .150, .200, .500, .1000, .5000, 10.000),
		},
		{
			Name:        "download_time_seconds",
			Measure:     census.mDownloadTime,
			Description: "Download time",
			TagKeys:     baseTagsWithManifestID,
			Aggregation: view.Distribution(0, .10, .20, .50, .100, .150, .200, .500, .1000, .5000, 10.000),
		},
		{
			Name:        "auth_webhook_time_milliseconds",
			Measure:     census.mAuthWebhookTime,
			Description: "Authentication webhook execution time, milliseconds",
			TagKeys:     baseTags,
			Aggregation: view.Distribution(0, 100, 250, 500, 750, 1000, 1500, 2000, 2500, 3000, 5000, 10000),
		},
		{
			Name:        "source_segment_duration_seconds",
			Measure:     census.mSourceSegmentDuration,
			Description: "Source segment's duration",
			TagKeys:     baseTagsWithManifestID,
			Aggregation: view.Distribution(0, .5, 1, 1.5, 2, 2.5, 3, 3.5, 4, 4.5, 5, 10, 15, 20),
		},
		{
			Name:        "max_sessions_total",
			Measure:     census.mMaxSessions,
			Description: "Max Sessions",
			TagKeys:     baseTags,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "current_sessions_total",
			Measure:     census.mCurrentSessions,
			Description: "Number of streams currently transcoding",
			TagKeys:     baseTags,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "discovery_errors_total",
			Measure:     census.mDiscoveryError,
			Description: "Number of discover errors",
			TagKeys:     append([]tag.Key{census.kErrorCode, census.kOrchestratorURI}, baseTags...),
			Aggregation: view.Count(),
		},
		{
			Name:        "transcode_retried",
			Measure:     census.mTranscodeRetried,
			Description: "Number of times segment transcode was retried",
			TagKeys:     append([]tag.Key{census.kTry}, baseTagsWithManifestID...),
			Aggregation: view.Count(),
		},
		{
			Name:        "transcoders_number",
			Measure:     census.mTranscodersNumber,
			Description: "Number of transcoders currently connected to orchestrator",
			TagKeys:     baseTags,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "transcoders_capacity",
			Measure:     census.mTranscodersCapacity,
			Description: "Total advertised capacity of transcoders currently connected to orchestrator",
			TagKeys:     baseTags,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "transcoders_load",
			Measure:     census.mTranscodersLoad,
			Description: "Total load of transcoders currently connected to orchestrator",
			TagKeys:     baseTags,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "orchestrator_swaps",
			Measure:     census.mOrchestratorSwaps,
			Description: "Number of orchestrator swaps mid-stream",
			TagKeys:     baseTagsWithManifestID,
			Aggregation: view.Count(),
		},

		// Metrics for sending payments
		{
			Name:        "ticket_value_sent",
			Measure:     census.mTicketValueSent,
			Description: "Ticket value sent",
			TagKeys:     baseTagsWithManifestID,
			Aggregation: view.Sum(),
		},
		{
			Name:        "tickets_sent",
			Measure:     census.mTicketsSent,
			Description: "Tickets sent",
			TagKeys:     baseTagsWithManifestID,
			Aggregation: view.Sum(),
		},
		{
			Name:        "payment_create_errors",
			Measure:     census.mPaymentCreateError,
			Description: "Errors when creating payments",
			TagKeys:     baseTagsWithManifestID,
			Aggregation: view.Sum(),
		},
		{
			Name:        "broadcaster_deposit",
			Measure:     census.mDeposit,
			Description: "Current remaining deposit for the broadcaster node",
			TagKeys:     baseTagsWithEthAddr,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "broadcaster_reserve",
			Measure:     census.mReserve,
			Description: "Current remaining reserve for the broadcaster node",
			TagKeys:     baseTagsWithEthAddr,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "max_transcoding_price",
			Measure:     census.mMaxTranscodingPrice,
			Description: "Maximum price per pixel to pay for transcoding",
			TagKeys:     baseTags,
			Aggregation: view.LastValue(),
		},

		// Metrics for receiving payments
		{
			Name:        "ticket_value_recv",
			Measure:     census.mTicketValueRecv,
			Description: "Ticket value received",
			TagKeys:     baseTagsWithManifestIDAndEthAddr,
			Aggregation: view.Sum(),
		},
		{
			Name:        "tickets_recv",
			Measure:     census.mTicketsRecv,
			Description: "Tickets received",
			TagKeys:     baseTagsWithManifestIDAndEthAddr,
			Aggregation: view.Sum(),
		},
		{
			Name:        "payment_recv_errors",
			Measure:     census.mPaymentRecvErr,
			Description: "Errors when receiving payments",
			TagKeys:     append([]tag.Key{census.kErrorCode}, baseTagsWithManifestIDAndEthAddr...),
			Aggregation: view.Sum(),
		},
		{
			Name:        "winning_tickets_recv",
			Measure:     census.mWinningTicketsRecv,
			Description: "Winning tickets received",
			TagKeys:     baseTagsWithManifestIDAndEthAddr,
			Aggregation: view.Sum(),
		},
		{
			Name:        "value_redeemed",
			Measure:     census.mValueRedeemed,
			Description: "Winning ticket value redeemed",
			TagKeys:     baseTagsWithEthAddr,
			Aggregation: view.Sum(),
		},
		{
			Name:        "ticket_redemption_errors",
			Measure:     census.mTicketRedemptionError,
			Description: "Errors when redeeming tickets",
			TagKeys:     baseTags,
			Aggregation: view.Sum(),
		},
		{
			Name:        "min_gas_price",
			Measure:     census.mMinGasPrice,
			Description: "Minimum gas price to use for gas price suggestions",
			TagKeys:     baseTags,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "max_gas_price",
			Measure:     census.mMaxGasPrice,
			Description: "Maximum gas price to use for gas price suggestions",
			TagKeys:     baseTags,
			Aggregation: view.LastValue(),
		},

		// Metrics for pixel accounting
		{
			Name:        "mil_pixels_processed",
			Measure:     census.mMilPixelsProcessed,
			Description: "Million pixels processed",
			TagKeys:     baseTagsWithManifestIDAndIP,
			Aggregation: view.Sum(),
		},
		{
			Name:        "suggested_gas_price",
			Measure:     census.mSuggestedGasPrice,
			Description: "Suggested gas price for winning ticket redemption",
			TagKeys:     baseTags,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "transcoding_price",
			Measure:     census.mTranscodingPrice,
			Description: "Transcoding price per pixel",
			TagKeys:     baseTagsWithEthAddr,
			Aggregation: view.LastValue(),
		},

		// Metrics for fast verification
		{
			Name:        "fast_verification_done",
			Measure:     census.mFastVerificationDone,
			Description: "Number of fast verifications done",
			TagKeys:     baseTagsWithManifestID,
			Aggregation: view.Count(),
		},
		{
			Name:        "fast_verification_failed",
			Measure:     census.mFastVerificationFailed,
			Description: "Number of fast verifications failed",
			TagKeys:     baseTagsWithManifestID,
			Aggregation: view.Count(),
		},
		{
			Name:        "fast_verification_enabled_current_sessions_total",
			Measure:     census.mFastVerificationEnabledCurrentSessions,
			Description: "Number of currently transcoded streams that have fast verification enabled",
			TagKeys:     baseTags,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "fast_verification_using_current_sessions_total",
			Measure:     census.mFastVerificationUsingCurrentSessions,
			Description: "Number of currently transcoded streams that have fast verification enabled and that are using an untrusted orchestrator",
			TagKeys:     baseTags,
			Aggregation: view.LastValue(),
		},
	}

	GlobalViews = views

	// Register the views
	if err := view.Register(views...); err != nil {
		glog.Fatalf("Failed to register views: %v", err)
	}
	registry := rprom.NewRegistry()
	registry.MustRegister(rprom.NewProcessCollector(rprom.ProcessCollectorOpts{}))
	registry.MustRegister(rprom.NewGoCollector())
	pe, err := prometheus.NewExporter(prometheus.Options{
		Namespace: "livepeer",
		Registry:  registry,
	})
	if err != nil {
		glog.Fatalf("Failed to create the Prometheus stats exporter: %v", err)
	}

	// Register the Prometheus exporters as a stats exporter.
	view.RegisterExporter(pe)
	stats.Record(ctx, mVersions.M(1))
	ctx, err = tag.New(census.ctx, tag.Insert(census.kErrorCode, "LostSegment"))
	if err != nil {
		glog.Fatal("Error creating context", err)
	}
	if !unitTestMode {
		go census.timeoutWatcher(ctx)
	}
	Exporter = pe

	// init metrics values
	SetTranscodersNumberAndLoad(0, 0, 0)
}

/*
func addManifestID(ctx context.Context, manifestID string) (context.Context, error) {
	if !PerStreamMetrics {
		return ctx, nil
	}
	return tag.New(ctx, tag.Insert(census.kManifestID, manifestID))
}
*/

func manifestIDTag(ctx context.Context, others ...tag.Mutator) []tag.Mutator {
	if PerStreamMetrics {
		others = append(others, tag.Insert(census.kManifestID, clog.GetManifestID(ctx)))
	}
	return others
}

func manifestIDTagStr(manifestID string, others ...tag.Mutator) []tag.Mutator {
	if PerStreamMetrics {
		others = append(others, tag.Insert(census.kManifestID, manifestID))
	}
	return others
}

func manifestIDTagAndIP(ctx context.Context, others ...tag.Mutator) []tag.Mutator {
	if PerStreamMetrics {
		others = append(others, tag.Insert(census.kManifestID, clog.GetManifestID(ctx)))
	}
	if ExposeClientIP {
		ip := clog.GetVal(ctx, clog.ClientIP)
		if ip == "" {
			ip = "not_tracked"
		}
		others = append(others, tag.Insert(census.kClientIP, ip))
	}
	return others
}

// LogDiscoveryError records discovery error
func LogDiscoveryError(ctx context.Context, uri, code string) {
	if strings.Contains(code, "OrchestratorCapped") {
		code = "OrchestratorCapped"
	} else if strings.Contains(code, "HTTP status code 404") {
		code = "HTTP 404"
	} else if strings.Contains(code, "DeadlineExceeded") || strings.Contains(code, "deadline") {
		code = "DeadlineExceeded"
	} else if strings.Contains(code, "Canceled") {
		code = "Canceled"
	}
	if code != "Canceled" {
		if err := stats.RecordWithTags(census.ctx,
			[]tag.Mutator{tag.Insert(census.kErrorCode, code),
				tag.Insert(census.kOrchestratorURI, uri)},
			census.mDiscoveryError.M(1)); err != nil {
			clog.Errorf(ctx, "Error recording metrics err=%q", err)
		}
	}
}

func (cen *CensusMetricsCounter) successRate() float64 {
	var i int
	var f float64
	if len(cen.success) == 0 {
		return 1
	}
	for _, avg := range cen.success {
		if r, has := avg.successRate(); has {
			if PerStreamMetrics {
				if err := stats.RecordWithTags(census.ctx,
					[]tag.Mutator{tag.Insert(census.kManifestID, avg.manifestID)}, census.mSuccessRatePerStream.M(r)); err != nil {

					glog.Errorf("Error recording metrics manifestID=%s err=%q", avg.manifestID, err)
				}
			}
			i++
			f += r
		}
	}
	if i > 0 {
		return f / float64(i)
	}
	return 1
}

func (sa *segmentsAverager) successRate() (float64, bool) {
	var emerged, transcoded int
	if sa.end == -1 {
		return 1, false
	}
	i := sa.start
	now := time.Now()
	for {
		item := &sa.segments[i]
		if item.transcoded > 0 || item.failed || now.Sub(item.emergedTime) > timeToWaitForError {
			emerged += item.emerged
			transcoded += item.transcoded
		}
		if i == sa.end {
			break
		}
		i = sa.advance(i)
	}
	if emerged > 0 {
		return float64(transcoded) / float64(emerged), true
	}
	return 1, false
}

func (sa *segmentsAverager) advance(i int) int {
	i++
	if i == len(sa.segments) {
		i = 0
	}
	return i
}

func (sa *segmentsAverager) addEmerged(seqNo uint64) {
	item, _ := sa.getAddItem(seqNo)
	item.emerged = 1
	item.transcoded = 0
	item.emergedTime = time.Now()
	item.seqNo = seqNo
}

func (sa *segmentsAverager) addTranscoded(seqNo uint64, failed bool) {
	item, found := sa.getAddItem(seqNo)
	if !found {
		item.emerged = 0
		item.emergedTime = time.Now()
	}
	item.failed = failed
	if !failed {
		item.transcoded = 1
	}
	item.seqNo = seqNo
}

func (sa *segmentsAverager) getAddItem(seqNo uint64) (*segmentCount, bool) {
	var index int
	if sa.end == -1 {
		sa.end = 0
	} else {
		i := sa.start
		for {
			if sa.segments[i].seqNo == seqNo {
				return &sa.segments[i], true
			}
			if i == sa.end {
				break
			}
			i = sa.advance(i)
		}
		sa.end = sa.advance(sa.end)
		index = sa.end
		if sa.end == sa.start {
			sa.start = sa.advance(sa.start)
		}
	}
	return &sa.segments[index], false
}

func (sa *segmentsAverager) canBeRemoved() bool {
	if sa.end == -1 {
		return true
	}
	i := sa.start
	now := time.Now()
	for {
		item := &sa.segments[i]
		if item.transcoded == 0 && !item.failed && now.Sub(item.emergedTime) <= timeToWaitForError {
			return false
		}
		if i == sa.end {
			break
		}
		i = sa.advance(i)
	}
	return true
}

func (cen *CensusMetricsCounter) timeoutWatcher(ctx context.Context) {
	for {
		cen.lock.Lock()
		now := time.Now()
		for nonce, emerged := range cen.emergeTimes {
			manifestID := "not_found"
			if avg, has := cen.success[nonce]; has {
				manifestID = avg.manifestID
			}
			for seqNo, tm := range emerged {
				ago := now.Sub(tm)
				if ago > timeToWaitForError {
					if err := stats.RecordWithTags(ctx, manifestIDTagStr(manifestID), census.mSegmentEmerged.M(1)); err != nil {
						glog.Errorf("Error recording metrics mnanifestID=%s err=%q", manifestID, err)
					}
					delete(emerged, seqNo)
					// This shouldn't happen, but if it is, we record
					// `LostSegment` error, to try to find out why we missed segment
					if err := stats.RecordWithTags(ctx, manifestIDTagStr(manifestID), census.mSegmentTranscodeFailed.M(1)); err != nil {
						glog.Errorf("Error recording metrics mnanifestID=%s err=%q", manifestID, err)
					}
					glog.Errorf("LostSegment nonce=%d seqNo=%d emerged=%ss ago", nonce, seqNo, ago)
				}
			}
		}
		cen.sendSuccess()
		for nonce, avg := range cen.success {
			if avg.removed && now.Sub(avg.removedAt) > 2*timeToWaitForError {
				// need to keep this around for some time to give Prometheus chance to scrape this value
				// (Prometheus scrapes every 5 seconds)
				delete(cen.success, nonce)
			} else {
				for seqNo, tr := range avg.tries {
					if now.Sub(tr.first) > 2*timeToWaitForError {
						delete(avg.tries, seqNo)
					}
				}
			}
		}
		cen.lock.Unlock()
		time.Sleep(timeoutWatcherPause)
	}
}

func MaxSessions(maxSessions int) {
	stats.Record(census.ctx, census.mMaxSessions.M(int64(maxSessions)))
}

func OrchestratorSwapped(ctx context.Context) {
	if err := stats.RecordWithTags(census.ctx, manifestIDTag(ctx), census.mOrchestratorSwaps.M(1)); err != nil {
		clog.Errorf(ctx, "Error recording metric err=%q", err)
	}
}

func CurrentSessions(currentSessions int) {
	stats.Record(census.ctx, census.mCurrentSessions.M(int64(currentSessions)))
}

func FastVerificationEnabledAndUsingCurrentSessions(enabled, using int) {
	stats.Record(census.ctx, census.mFastVerificationEnabledCurrentSessions.M(int64(enabled)), census.mFastVerificationUsingCurrentSessions.M(int64(using)))
}

func TranscodeTry(ctx context.Context, nonce, seqNo uint64) {
	census.lock.Lock()
	defer census.lock.Unlock()
	if av, ok := census.success[nonce]; ok {
		if av.tries == nil {
			av.tries = make(map[uint64]tryData)
		}
		try := 1
		if ts, tok := av.tries[seqNo]; tok {
			ts.tries++
			try = ts.tries
			av.tries[seqNo] = ts
			label := ">10"
			if ts.tries < 11 {
				label = strconv.Itoa(ts.tries)
			}
			if err := stats.RecordWithTags(census.ctx, manifestIDTag(ctx, tag.Insert(census.kTry, label)), census.mTranscodeRetried.M(1)); err != nil {
				clog.Errorf(ctx, "Error recording metrics err=%q", err)
				return
			}
		} else {
			av.tries[seqNo] = tryData{tries: 1, first: time.Now()}
		}
		clog.V(logLevel).Infof(ctx, "Trying to transcode segment nonce=%d seqNo=%d try=%d", nonce, seqNo, try)
	}
}

func SetTranscodersNumberAndLoad(load, capacity, number int) {
	stats.Record(census.ctx, census.mTranscodersLoad.M(int64(load)))
	stats.Record(census.ctx, census.mTranscodersCapacity.M(int64(capacity)))
	stats.Record(census.ctx, census.mTranscodersNumber.M(int64(number)))
}

func SegmentEmerged(ctx context.Context, nonce, seqNo uint64, profilesNum int, dur float64) {
	clog.V(logLevel).Infof(ctx, "Logging SegmentEmerged... duration=%v", dur)
	if err := stats.RecordWithTags(census.ctx,
		manifestIDTagAndIP(ctx),
		census.mSegmentEmergedUnprocessed.M(1)); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
	if census.nodeType == Broadcaster {
		census.segmentEmerged(nonce, seqNo, profilesNum)
	}
	if err := stats.RecordWithTags(census.ctx, manifestIDTag(ctx), census.mSourceSegmentDuration.M(dur)); err != nil {
		clog.Errorf(ctx, "Error recording metric err=%q", err)
	}
}

func (cen *CensusMetricsCounter) segmentEmerged(nonce, seqNo uint64, profilesNum int) {
	cen.lock.Lock()
	defer cen.lock.Unlock()
	if _, has := cen.emergeTimes[nonce]; !has {
		cen.emergeTimes[nonce] = make(map[uint64]time.Time)
	}
	if avg, has := cen.success[nonce]; has {
		avg.addEmerged(seqNo)
	}
	cen.emergeTimes[nonce][seqNo] = time.Now()
}

func SourceSegmentAppeared(ctx context.Context, nonce, seqNo uint64, manifestID, profile string, recordingEnabled bool) {
	clog.V(logLevel).Infof(ctx, "Logging SourceSegmentAppeared... profile=%s", profile)
	census.segmentSourceAppeared(ctx, nonce, seqNo, profile, recordingEnabled)
}

func (cen *CensusMetricsCounter) segmentSourceAppeared(ctx context.Context, nonce, seqNo uint64, profile string, recordingEnabled bool) {
	segType := segTypeRegular
	if recordingEnabled {
		segType = segTypeRec
	}
	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx,
			tag.Insert(cen.kSegmentType, segType),
			tag.Insert(census.kProfile, profile)),
		census.mSegmentSourceAppeared.M(1)); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
}

func SegmentUploaded(ctx context.Context, nonce, seqNo uint64, uploadDur time.Duration, uri string) {
	clog.V(logLevel).Infof(ctx, "Logging SegmentUploaded... dur=%s", uploadDur)

	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx,
			tag.Insert(census.kOrchestratorURI, uri)),
		census.mSegmentUploaded.M(1)); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
	if err := stats.RecordWithTags(census.ctx,
		[]tag.Mutator{tag.Insert(census.kOrchestratorURI, uri)},
		census.mUploadTime.M(uploadDur.Seconds())); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
}

func SegmentDownloaded(ctx context.Context, nonce, seqNo uint64, downloadDur time.Duration) {
	clog.V(logLevel).Infof(ctx, "Logging SegmentDownloaded... dur=%s", downloadDur)

	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx),
		census.mSegmentDownloaded.M(1),
		census.mDownloadTime.M(downloadDur.Seconds())); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
}

func HTTPClientTimedOut1(ctx context.Context) {
	if err := stats.RecordWithTags(census.ctx,
		manifestIDTagAndIP(ctx),
		census.mHTTPClientTimeout1.M(1)); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
}

func HTTPClientTimedOut2(ctx context.Context) {
	if err := stats.RecordWithTags(census.ctx,
		manifestIDTagAndIP(ctx),
		census.mHTTPClientTimeout2.M(1)); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
}

func SegmentFullyProcessed(ctx context.Context, segDur, processDur float64) {
	if processDur == 0 {
		return
	}
	ratio := segDur / processDur
	var bucketM stats.Measurement
	switch {
	case ratio > 3:
		bucketM = census.mRealtime3x.M(1)
	case ratio > 2:
		bucketM = census.mRealtime2x.M(1)
	case ratio > 1:
		bucketM = census.mRealtime1x.M(1)
	case ratio > 0.5:
		bucketM = census.mRealtimeHalf.M(1)
	default:
		bucketM = census.mRealtimeSlow.M(1)
	}
	if err := stats.RecordWithTags(census.ctx,
		manifestIDTagAndIP(ctx),
		bucketM, census.mRealtimeRatio.M(ratio)); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
}

func AuthWebhookFinished(dur time.Duration) {
	census.authWebhookFinished(dur)
}

func (cen *CensusMetricsCounter) authWebhookFinished(dur time.Duration) {
	stats.Record(cen.ctx, cen.mAuthWebhookTime.M(float64(dur)/float64(time.Millisecond)))
}

func SegmentUploadFailed(ctx context.Context, nonce, seqNo uint64, code SegmentUploadError, err error, permanent bool,
	uri string) {

	if code == SegmentUploadErrorUnknown {
		reason := err.Error()
		var timedout bool
		if err, ok := err.(net.Error); ok && err.Timeout() {
			timedout = true
		}
		if timedout || strings.Contains(reason, "Client.Timeout") || strings.Contains(reason, "timed out") ||
			strings.Contains(reason, "timeout") || strings.Contains(reason, "context deadline exceeded") ||
			strings.Contains(reason, "EOF") {
			code = SegmentUploadErrorTimeout
		} else if reason == "Session ended" {
			code = SegmentUploadErrorSessionEnded
		}
	}
	clog.Errorf(ctx, "Logging SegmentUploadFailed... code=%v reason='%s'", code, err.Error())

	census.segmentUploadFailed(ctx, nonce, seqNo, code, permanent, uri)
}

func (cen *CensusMetricsCounter) segmentUploadFailed(ctx context.Context, nonce, seqNo uint64, code SegmentUploadError, permanent bool,
	uri string) {

	cen.lock.Lock()
	defer cen.lock.Unlock()
	if permanent {
		cen.countSegmentEmerged(ctx, nonce, seqNo)
	}

	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx, tag.Insert(census.kOrchestratorURI, uri), tag.Insert(census.kErrorCode, string(code))),
		census.mSegmentUploadFailed.M(1)); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}

	if permanent {
		cen.countSegmentTranscoded(nonce, seqNo, true)
		cen.sendSuccess()
	}
}

func SegmentTranscoded(ctx context.Context, nonce, seqNo uint64, sourceDur time.Duration, transcodeDur time.Duration, profiles string,
	trusted, verified bool) {

	clog.V(logLevel).Infof(ctx, "Logging SegmentTranscode nonce=%d seqNo=%d dur=%s trusted=%v verified=%v", nonce, seqNo, transcodeDur, trusted, verified)
	census.segmentTranscoded(nonce, seqNo, sourceDur, transcodeDur, profiles, trusted, verified)
}

func (cen *CensusMetricsCounter) segmentTranscoded(nonce, seqNo uint64, sourceDur time.Duration, transcodeDur time.Duration,
	profiles string, trusted, verified bool) {

	cen.lock.Lock()
	defer cen.lock.Unlock()
	verifiedStr := "verified"
	if !verified {
		verifiedStr = "unverified"
	}
	trustedStr := "trusted"
	if !trusted {
		trustedStr = "untrusted"
	}
	ctx, err := tag.New(cen.ctx, tag.Insert(cen.kProfiles, profiles), tag.Insert(cen.kVerified, verifiedStr), tag.Insert(cen.kTrusted, trustedStr))
	if err != nil {
		glog.Error("Error creating context", err)
		return
	}
	stats.Record(ctx, cen.mSegmentTranscoded.M(1), cen.mTranscodeTime.M(transcodeDur.Seconds()), cen.mTranscodeScore.M(sourceDur.Seconds()/transcodeDur.Seconds()))
}

func SegmentTranscodeFailed(ctx context.Context, subType SegmentTranscodeError, nonce, seqNo uint64, err error, permanent bool) {
	clog.Errorf(ctx, "Logging SegmentTranscodeFailed subtype=%v err=%q", subType, err.Error())
	census.segmentTranscodeFailed(ctx, nonce, seqNo, subType, permanent)
}

func (cen *CensusMetricsCounter) segmentTranscodeFailed(ctx context.Context, nonce, seqNo uint64, code SegmentTranscodeError, permanent bool) {
	cen.lock.Lock()
	defer cen.lock.Unlock()
	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx, tag.Insert(census.kErrorCode, string(code))), cen.mSegmentTranscodeFailed.M(1)); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}

	if permanent {
		cen.countSegmentEmerged(ctx, nonce, seqNo)
		cen.countSegmentTranscoded(nonce, seqNo, code != SegmentTranscodeErrorSessionEnded)
		cen.sendSuccess()
	}
}

func (cen *CensusMetricsCounter) countSegmentTranscoded(nonce, seqNo uint64, failed bool) {
	if avg, ok := cen.success[nonce]; ok {
		avg.addTranscoded(seqNo, failed)
	}
}

func (cen *CensusMetricsCounter) countSegmentEmerged(ctx context.Context, nonce, seqNo uint64) {
	if _, ok := cen.emergeTimes[nonce][seqNo]; ok {
		if err := stats.RecordWithTags(cen.ctx,
			manifestIDTag(ctx), cen.mSegmentEmerged.M(1)); err != nil {
			clog.Errorf(ctx, "Error recording metrics err=%q", err)
		}
		delete(cen.emergeTimes[nonce], seqNo)
	}
}

func (cen *CensusMetricsCounter) sendSuccess() {
	stats.Record(cen.ctx, cen.mSuccessRate.M(cen.successRate()))
}

func SegmentFullyTranscoded(ctx context.Context, nonce, seqNo uint64, profiles string, errCode SegmentTranscodeError) {
	census.lock.Lock()
	defer census.lock.Unlock()
	rctx, err := tag.New(census.ctx, tag.Insert(census.kProfiles, profiles))
	if err != nil {
		glog.Error("Error creating context", err)
		return
	}

	if st, ok := census.emergeTimes[nonce][seqNo]; ok {
		if errCode == "" {
			latency := time.Since(st)
			if err := stats.RecordWithTags(rctx,
				manifestIDTag(ctx), census.mTranscodeOverallLatency.M(latency.Seconds())); err != nil {
				clog.Errorf(ctx, "Error recording metrics err=%q", err)
			}
		}
		census.countSegmentEmerged(ctx, nonce, seqNo)
	}
	if errCode == "" {
		if err := stats.RecordWithTags(rctx,
			manifestIDTag(ctx), census.mSegmentTranscodedAllAppeared.M(1)); err != nil {
			clog.Errorf(ctx, "Error recording metrics err=%q", err)
		}
	}
	failed := errCode != "" && errCode != SegmentTranscodeErrorSessionEnded
	census.countSegmentTranscoded(nonce, seqNo, failed)
	if !failed {
		if err := stats.RecordWithTags(rctx,
			manifestIDTag(ctx), census.mSegmentTranscodedUnprocessed.M(1)); err != nil {
			clog.Errorf(ctx, "Error recording metrics err=%q", err)
		}
	}
	census.sendSuccess()
}

func RecordingPlaylistSaved(dur time.Duration, err error) {
	if err != nil {
		stats.Record(census.ctx, census.mRecordingSaveErrors.M(1))
	} else {
		stats.Record(census.ctx, census.mRecordingSaveLatency.M(dur.Seconds()))
	}
}

func RecordingSegmentSaved(dur time.Duration, err error) {
	if err != nil {
		stats.Record(census.ctx, census.mRecordingSaveErrors.M(1))
	} else {
		stats.Record(census.ctx, census.mRecordingSaveLatency.M(dur.Seconds()))
		stats.Record(census.ctx, census.mRecordingSavedSegments.M(1))
	}
}

func StreamCreateFailed(nonce uint64, reason string) {
	glog.Errorf("Logging StreamCreateFailed... nonce=%d reason='%s'", nonce, reason)
	stats.Record(census.ctx, census.mStreamCreateFailed.M(1))
}

func newAverager(manifestID string) *segmentsAverager {
	return &segmentsAverager{
		manifestID: manifestID,
		segments:   make([]segmentCount, numberOfSegmentsToCalcAverage),
		end:        -1,
	}
}

func StreamCreated(manifestID string, nonce uint64) {
	glog.V(logLevel).Infof("Logging StreamCreated... nonce=%d manifestID=%s", nonce, manifestID)
	census.streamCreated(manifestID, nonce)
}

func (cen *CensusMetricsCounter) streamCreated(manifestID string, nonce uint64) {
	cen.lock.Lock()
	defer cen.lock.Unlock()
	stats.Record(cen.ctx, cen.mStreamCreated.M(1))
	cen.success[nonce] = newAverager(manifestID)
}

func StreamStarted(nonce uint64) {
	glog.V(logLevel).Infof("Logging StreamStarted... nonce=%d", nonce)
	census.streamStarted(nonce)
}

func (cen *CensusMetricsCounter) streamStarted(nonce uint64) {
	stats.Record(cen.ctx, cen.mStreamStarted.M(1))
}

func StreamEnded(ctx context.Context, nonce uint64) {
	clog.V(logLevel).Infof(ctx, "Logging StreamEnded... nonce=%d", nonce)
	census.streamEnded(nonce)
}

func (cen *CensusMetricsCounter) streamEnded(nonce uint64) {
	cen.lock.Lock()
	defer cen.lock.Unlock()
	stats.Record(cen.ctx, cen.mStreamEnded.M(1))
	delete(cen.emergeTimes, nonce)
	if avg, has := cen.success[nonce]; has {
		if avg.canBeRemoved() {
			delete(cen.success, nonce)
		} else {
			avg.removed = true
			avg.removedAt = time.Now()
		}
	}
	census.sendSuccess()
}

// TicketValueSent records the ticket value sent
func TicketValueSent(ctx context.Context, value *big.Rat) {
	if value.Cmp(big.NewRat(0, 1)) <= 0 {
		return
	}

	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx), census.mTicketValueSent.M(fracwei2gwei(value))); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
}

// TicketsSent records the number of tickets sent
func TicketsSent(ctx context.Context, numTickets int) {
	if numTickets <= 0 {
		return
	}

	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx), census.mTicketsSent.M(int64(numTickets))); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
}

// PaymentCreateError records a error from payment creation
func PaymentCreateError(ctx context.Context) {
	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx), census.mPaymentCreateError.M(1)); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
}

// Deposit records the current deposit for the broadcaster
func Deposit(sender string, deposit *big.Int) {
	if err := stats.RecordWithTags(census.ctx,
		[]tag.Mutator{tag.Insert(census.kSender, sender)}, census.mDeposit.M(wei2gwei(deposit))); err != nil {

		glog.Errorf("Error recording metrics err=%q", err)
	}
}

func Reserve(sender string, reserve *big.Int) {
	if err := stats.RecordWithTags(census.ctx,
		[]tag.Mutator{tag.Insert(census.kSender, sender)}, census.mReserve.M(wei2gwei(reserve))); err != nil {

		glog.Errorf("Error recording metrics err=%q", err)
	}
}

func MaxTranscodingPrice(maxPrice *big.Rat) {
	floatWei, ok := maxPrice.Float64()
	if ok {
		if err := stats.RecordWithTags(census.ctx,
			[]tag.Mutator{tag.Insert(census.kSender, "max")},
			census.mTranscodingPrice.M(floatWei)); err != nil {

			glog.Errorf("Error recording metrics err=%q", err)
		}
	}
}

// TicketValueRecv records the ticket value received from a sender for a manifestID
func TicketValueRecv(ctx context.Context, sender string, value *big.Rat) {
	if value.Cmp(big.NewRat(0, 1)) <= 0 {
		return
	}

	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx, tag.Insert(census.kSender, sender)), census.mTicketValueRecv.M(fracwei2gwei(value))); err != nil {

		glog.Errorf("Error recording metrics err=%q", err)
	}
}

// TicketsRecv records the number of tickets received from a sender for a manifestID
func TicketsRecv(ctx context.Context, sender string, numTickets int) {
	if numTickets <= 0 {
		return
	}

	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx, tag.Insert(census.kSender, sender)), census.mTicketsRecv.M(int64(numTickets))); err != nil {

		glog.Errorf("Error recording metrics err=%q", err)
	}
}

// PaymentRecvError records an error from receiving a payment
func PaymentRecvError(ctx context.Context, sender string, errStr string) {

	var errCode string
	if strings.Contains(errStr, "Expected price") {
		errCode = "InvalidPrice"
	} else if strings.Contains(errStr, "invalid already revealed recipientRand") {
		errCode = "InvalidRecipientRand"
	} else if strings.Contains(errStr, "invalid ticket faceValue") {
		errCode = "InvalidTicketFaceValue"
	} else if strings.Contains(errStr, "invalid ticket winProb") {
		errCode = "InvalidTicketWinProb"
	} else {
		errCode = "PaymentError"
	}

	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx, tag.Insert(census.kErrorCode, errCode), tag.Insert(census.kSender, sender)),
		census.mPaymentRecvErr.M(1)); err != nil {

		glog.Errorf("Error recording metrics err=%q", err)
	}
}

// WinningTicketsRecv records the number of winning tickets received
func WinningTicketsRecv(ctx context.Context, sender string, numTickets int) {
	if numTickets <= 0 {
		return
	}

	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx, tag.Insert(census.kSender, sender)),
		census.mWinningTicketsRecv.M(int64(numTickets))); err != nil {

		glog.Errorf("Error recording metrics err=%q", err)
	}
}

// ValueRedeemed records the value from redeeming winning tickets
func ValueRedeemed(sender string, value *big.Int) {
	if value.Cmp(big.NewInt(0)) <= 0 {
		return
	}

	if err := stats.RecordWithTags(census.ctx,
		[]tag.Mutator{tag.Insert(census.kSender, sender)},
		census.mValueRedeemed.M(wei2gwei(value))); err != nil {

		glog.Errorf("Error recording metrics err=%q", err)
	}
}

// TicketRedemptionError records an error from redeeming a ticket
func TicketRedemptionError(sender string) {
	if err := stats.RecordWithTags(census.ctx,
		[]tag.Mutator{tag.Insert(census.kSender, sender)},
		census.mTicketRedemptionError.M(1)); err != nil {

		glog.Errorf("Error recording metrics err=%q", err)
	}
}

func MilPixelsProcessed(ctx context.Context, milPixels float64) {
	if err := stats.RecordWithTags(census.ctx,
		manifestIDTagAndIP(ctx), census.mMilPixelsProcessed.M(milPixels)); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
}

// SuggestedGasPrice records the last suggested gas price
func SuggestedGasPrice(gasPrice *big.Int) {
	stats.Record(census.ctx, census.mSuggestedGasPrice.M(wei2gwei(gasPrice)))
}

func MinGasPrice(minGasPrice *big.Int) {
	stats.Record(census.ctx, census.mMinGasPrice.M(wei2gwei(minGasPrice)))
}

func MaxGasPrice(maxGasPrice *big.Int) {
	stats.Record(census.ctx, census.mMaxGasPrice.M(wei2gwei(maxGasPrice)))
}

// TranscodingPrice records the last transcoding price
func TranscodingPrice(sender string, price *big.Rat) {
	floatWei, ok := price.Float64()
	if ok {
		stats.Record(census.ctx, census.mTranscodingPrice.M(floatWei))
		if err := stats.RecordWithTags(census.ctx,
			[]tag.Mutator{tag.Insert(census.kSender, sender)},
			census.mTranscodingPrice.M(floatWei)); err != nil {

			glog.Errorf("Error recording metrics err=%q", err)
		}
	}
}

// Convert wei to gwei
func wei2gwei(wei *big.Int) float64 {
	gwei, _ := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(float64(gweiConversionFactor))).Float64()
	return gwei
}

// Convert fractional wei to gwei
func fracwei2gwei(wei *big.Rat) float64 {
	floatWei, _ := wei.Float64()
	return floatWei / gweiConversionFactor
}

func FastVerificationDone(ctx context.Context) {
	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx), census.mFastVerificationDone.M(1)); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
}

func FastVerificationFailed(ctx context.Context) {
	if err := stats.RecordWithTags(census.ctx,
		manifestIDTag(ctx), census.mFastVerificationFailed.M(1)); err != nil {
		clog.Errorf(ctx, "Error recording metrics err=%q", err)
	}
}

func PrintMetricsMarkdown() {
	fmt.Println("\n## Livepeer metrics\n")
	fmt.Println("### General")
	printHeader()
	printMetric("versions", "Version information.", "Num")
	printMetric(census.mSegmentSourceAppeared.Name(), census.mSegmentSourceAppeared.Description(), census.mSegmentSourceAppeared.Unit())
	printMetric(census.mSegmentEmerged.Name(), census.mSegmentEmerged.Description(), census.mSegmentEmerged.Unit())
	printMetric(census.mSegmentEmergedUnprocessed.Name(), census.mSegmentEmergedUnprocessed.Description(), census.mSegmentEmergedUnprocessed.Unit())
	printMetric(census.mSegmentUploaded.Name(), census.mSegmentUploaded.Description(), census.mSegmentUploaded.Unit())
	printMetric(census.mSegmentUploadFailed.Name(), census.mSegmentUploadFailed.Description(), census.mSegmentUploadFailed.Unit())
	printMetric(census.mSegmentDownloaded.Name(), census.mSegmentDownloaded.Description(), census.mSegmentDownloaded.Unit())
	printMetric(census.mSegmentTranscoded.Name(), census.mSegmentTranscoded.Description(), census.mSegmentTranscoded.Unit())
	printMetric(census.mSegmentTranscodedUnprocessed.Name(), census.mSegmentTranscodedUnprocessed.Description(), census.mSegmentTranscodedUnprocessed.Unit())
	printMetric(census.mSegmentTranscodeFailed.Name(), census.mSegmentTranscodeFailed.Description(), census.mSegmentTranscodeFailed.Unit())
	printMetric(census.mSegmentTranscodedAppeared.Name(), census.mSegmentTranscodedAppeared.Description(), census.mSegmentTranscodedAppeared.Unit())
	printMetric(census.mSegmentTranscodedAllAppeared.Name(), census.mSegmentTranscodedAllAppeared.Description(), census.mSegmentTranscodedAllAppeared.Unit())
	printMetric(census.mStartBroadcastClientFailed.Name(), census.mStartBroadcastClientFailed.Description(), census.mStartBroadcastClientFailed.Unit())
	printMetric(census.mStreamCreateFailed.Name(), census.mStreamCreateFailed.Description(), census.mStreamCreateFailed.Unit())
	printMetric(census.mStreamCreated.Name(), census.mStreamCreated.Description(), census.mStreamCreated.Unit())
	printMetric(census.mStreamStarted.Name(), census.mStreamStarted.Description(), census.mStreamStarted.Unit())
	printMetric(census.mStreamEnded.Name(), census.mStreamEnded.Description(), census.mStreamEnded.Unit())
	printMetric(census.mMaxSessions.Name(), census.mMaxSessions.Description(), census.mMaxSessions.Unit())
	printMetric(census.mCurrentSessions.Name(), census.mCurrentSessions.Description(), census.mCurrentSessions.Unit())
	printMetric(census.mDiscoveryError.Name(), census.mDiscoveryError.Description(), census.mDiscoveryError.Unit())
	printMetric(census.mTranscodeRetried.Name(), census.mTranscodeRetried.Description(), census.mTranscodeRetried.Unit())
	printMetric(census.mTranscodersNumber.Name(), census.mTranscodersNumber.Description(), census.mTranscodersNumber.Unit())
	printMetric(census.mTranscodersCapacity.Name(), census.mTranscodersCapacity.Description(), census.mTranscodersCapacity.Unit())
	printMetric(census.mTranscodersLoad.Name(), census.mTranscodersLoad.Description(), census.mTranscodersLoad.Unit())
	printMetric(census.mSuccessRate.Name(), census.mSuccessRate.Description(), census.mSuccessRate.Unit())
	printMetric(census.mSuccessRatePerStream.Name(), census.mSuccessRatePerStream.Description(), census.mSuccessRatePerStream.Unit())
	printMetric(census.mTranscodeTime.Name(), census.mTranscodeTime.Description(), census.mTranscodeTime.Unit())
	printMetric(census.mTranscodeLatency.Name(), census.mTranscodeLatency.Description(), census.mTranscodeLatency.Unit())
	printMetric(census.mTranscodeOverallLatency.Name(), census.mTranscodeOverallLatency.Description(), census.mTranscodeOverallLatency.Unit())
	printMetric(census.mUploadTime.Name(), census.mUploadTime.Description(), census.mUploadTime.Unit())
	printMetric(census.mDownloadTime.Name(), census.mDownloadTime.Description(), census.mDownloadTime.Unit())
	printMetric(census.mAuthWebhookTime.Name(), census.mAuthWebhookTime.Description(), census.mAuthWebhookTime.Unit())
	printMetric(census.mSourceSegmentDuration.Name(), census.mSourceSegmentDuration.Description(), census.mSourceSegmentDuration.Unit())
	printMetric(census.mHTTPClientTimeout1.Name(), census.mHTTPClientTimeout1.Description(), census.mHTTPClientTimeout1.Unit())
	printMetric(census.mHTTPClientTimeout2.Name(), census.mHTTPClientTimeout2.Description(), census.mHTTPClientTimeout2.Unit())
	printMetric(census.mRealtimeRatio.Name(), census.mRealtimeRatio.Description(), census.mRealtimeRatio.Unit())
	printMetric(census.mRealtime3x.Name(), census.mRealtime3x.Description(), census.mRealtime3x.Unit())
	printMetric(census.mRealtime2x.Name(), census.mRealtime2x.Description(), census.mRealtime2x.Unit())
	printMetric(census.mRealtime1x.Name(), census.mRealtime1x.Description(), census.mRealtime1x.Unit())
	printMetric(census.mRealtimeHalf.Name(), census.mRealtimeHalf.Description(), census.mRealtimeHalf.Unit())
	printMetric(census.mRealtimeSlow.Name(), census.mRealtimeSlow.Description(), census.mRealtimeSlow.Unit())
	printMetric(census.mTranscodeScore.Name(), census.mTranscodeScore.Description(), census.mTranscodeScore.Unit())
	printMetric(census.mRecordingSaveLatency.Name(), census.mRecordingSaveLatency.Description(), census.mRecordingSaveLatency.Unit())
	printMetric(census.mRecordingSaveErrors.Name(), census.mRecordingSaveErrors.Description(), census.mRecordingSaveErrors.Unit())
	printMetric(census.mRecordingSavedSegments.Name(), census.mRecordingSavedSegments.Description(), census.mRecordingSavedSegments.Unit())
	printMetric(census.mOrchestratorSwaps.Name(), census.mOrchestratorSwaps.Description(), census.mOrchestratorSwaps.Unit())

	fmt.Println("\n### Sending payments")
	printHeader()
	printMetric(census.mTicketValueSent.Name(), census.mTicketValueSent.Description(), census.mTicketValueSent.Unit())
	printMetric(census.mTicketsSent.Name(), census.mTicketsSent.Description(), census.mTicketsSent.Unit())
	printMetric(census.mPaymentCreateError.Name(), census.mPaymentCreateError.Description(), census.mPaymentCreateError.Unit())
	printMetric(census.mDeposit.Name(), census.mDeposit.Description(), census.mDeposit.Unit())
	printMetric(census.mReserve.Name(), census.mReserve.Description(), census.mReserve.Unit())
	printMetric(census.mMaxTranscodingPrice.Name(), census.mMaxTranscodingPrice.Description(), census.mMaxTranscodingPrice.Unit())

	fmt.Println("\n### Receiving payments")
	printHeader()
	printMetric(census.mTicketValueRecv.Name(), census.mTicketValueRecv.Description(), census.mTicketValueRecv.Unit())
	printMetric(census.mTicketsRecv.Name(), census.mTicketsRecv.Description(), census.mTicketsRecv.Unit())
	printMetric(census.mPaymentRecvErr.Name(), census.mPaymentRecvErr.Description(), census.mPaymentRecvErr.Unit())
	printMetric(census.mWinningTicketsRecv.Name(), census.mWinningTicketsRecv.Description(), census.mWinningTicketsRecv.Unit())
	printMetric(census.mValueRedeemed.Name(), census.mValueRedeemed.Description(), census.mValueRedeemed.Unit())
	printMetric(census.mTicketRedemptionError.Name(), census.mTicketRedemptionError.Description(), census.mTicketRedemptionError.Unit())
	printMetric(census.mSuggestedGasPrice.Name(), census.mSuggestedGasPrice.Description(), census.mSuggestedGasPrice.Unit())
	printMetric(census.mMinGasPrice.Name(), census.mMinGasPrice.Description(), census.mMinGasPrice.Unit())
	printMetric(census.mMaxGasPrice.Name(), census.mMaxGasPrice.Description(), census.mMaxGasPrice.Unit())
	printMetric(census.mTranscodingPrice.Name(), census.mTranscodingPrice.Description(), census.mTranscodingPrice.Unit())

	fmt.Println("\n### Pixel accounting")
	printHeader()
	printMetric(census.mMilPixelsProcessed.Name(), census.mMilPixelsProcessed.Description(), census.mMilPixelsProcessed.Unit())

	fmt.Println("\n### Fast verification")
	printHeader()
	printMetric(census.mFastVerificationDone.Name(), census.mFastVerificationDone.Description(), census.mFastVerificationDone.Unit())
	printMetric(census.mFastVerificationFailed.Name(), census.mFastVerificationFailed.Description(), census.mFastVerificationFailed.Unit())
	printMetric(census.mFastVerificationEnabledCurrentSessions.Name(), census.mFastVerificationEnabledCurrentSessions.Description(), census.mFastVerificationEnabledCurrentSessions.Unit())
	printMetric(census.mFastVerificationUsingCurrentSessions.Name(), census.mFastVerificationUsingCurrentSessions.Description(), census.mFastVerificationUsingCurrentSessions.Unit())

	fmt.Println("\n## Golang metrics\n")
	printGolangHeader()
	printGolangMetric("go_gc_duration_seconds", "A summary of the pause duration of garbage collection cycles.", "summary")
	printGolangMetric("go_goroutines", "Number of goroutines that currently exist.", "gauge")
	printGolangMetric("go_info", "Information about the Go environment.", "gauge")
	printGolangMetric("go_memstats_alloc_bytes", "Number of bytes allocated and still in use.", "gauge")
	printGolangMetric("go_memstats_alloc_bytes_total", "Total number of bytes allocated, even if freed.", "counter")
	printGolangMetric("go_memstats_buck_hash_sys_bytes", "Number of bytes used by the profiling bucket hash table.", "gauge")
	printGolangMetric("go_memstats_frees_total", "Total number of frees.", "counter")
	printGolangMetric("go_memstats_gc_cpu_fraction", "The fraction of this program's available CPU time used by the GC since the program started.", "gauge")
	printGolangMetric("go_memstats_gc_sys_bytes", "Number of bytes used for garbage collection system metadata.", "gauge")
	printGolangMetric("go_memstats_heap_alloc_bytes", "Number of heap bytes allocated and still in use.", "gauge")
	printGolangMetric("go_memstats_heap_idle_bytes", "Number of heap bytes waiting to be used.", "gauge")
	printGolangMetric("go_memstats_heap_inuse_bytes", "Number of heap bytes that are in use.", "gauge")
	printGolangMetric("go_memstats_heap_objects", "Number of allocated objects.", "gauge")
	printGolangMetric("go_memstats_heap_released_bytes", "Number of heap bytes released to OS.", "gauge")
	printGolangMetric("go_memstats_heap_sys_bytes", "Number of heap bytes obtained from system.", "gauge")
	printGolangMetric("go_memstats_last_gc_time_seconds", "Number of seconds since 1970 of last garbage collection.", "gauge")
	printGolangMetric("go_memstats_lookups_total", "Total number of pointer lookups.", "counter")
	printGolangMetric("go_memstats_mallocs_total", "Total number of mallocs.", "counter")
	printGolangMetric("go_memstats_mcache_inuse_bytes", "Number of bytes in use by mcache structures.", "gauge")
	printGolangMetric("go_memstats_mcache_sys_bytes", "Number of bytes used for mcache structures obtained from system.", "gauge")
	printGolangMetric("go_memstats_mspan_inuse_bytes", "Number of bytes in use by mspan structures.", "gauge")
	printGolangMetric("go_memstats_mspan_sys_bytes", "Number of bytes used for mspan structures obtained from system.", "gauge")
	printGolangMetric("go_memstats_next_gc_bytes", "Number of heap bytes when next garbage collection will take place.", "gauge")
	printGolangMetric("go_memstats_other_sys_bytes", "Number of bytes used for other system allocations.", "gauge")
	printGolangMetric("go_memstats_stack_inuse_bytes", "Number of bytes in use by the stack allocator.", "gauge")
	printGolangMetric("go_memstats_stack_sys_bytes", "Number of bytes obtained from system for stack allocator.", "gauge")
	printGolangMetric("go_memstats_sys_bytes", "Number of bytes obtained from system.", "gauge")
	printGolangMetric("go_threads", "Number of OS threads created.", "gauge")
}

func printHeader() {
	header := `
| Name         | Description      | Unit | Type |
| ------------ | ---------------- | ---- | ---- |`
	fmt.Println(header)
}

func printMetric(name string, description string, unit string) {
	vDescription, vType := findInViews(name)
	if vDescription != "" {
		description = vDescription
	}

	fmt.Printf("| `livepeer_%s` | %s | %s | %s |\n", name, description, unit, vType)
}

func findInViews(name string) (string, string) {
	for _, v := range GlobalViews {
		if v.Name == name {
			return v.Description, toType(v.Aggregation)
		}
	}
	return "", ""
}

func toType(aggregation *view.Aggregation) string {
	switch aggregation.Type {
	case view.AggTypeCount:
		return "counter"
	case view.AggTypeSum:
		return "counter"
	case view.AggTypeDistribution:
		return "histogram"
	case view.AggTypeLastValue:
		return "gauge"

	}
	return ""
}

func printGolangHeader() {
	header := `
| Name         | Description      | Type |
| ------------ | ---------------- | ---- |`
	fmt.Println(header)
}

func printGolangMetric(name string, description string, mType string) {
	fmt.Printf("| `%s` | %s | %s |\n", name, description, mType)
}
