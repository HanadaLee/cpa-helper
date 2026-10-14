package app

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const (
	keeperUsageURL                 = "https://chatgpt.com/backend-api/wham/usage"
	keeperResetCreditsURL          = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	keeperResetCreditsConsumeURL   = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"
	keeperLogFilePrefix            = "codex-keeper-"
	keeperLogComponent             = "codex_keeper"
	keeperLogRetainedFiles         = 3
	keeperMaxInMemoryLogs          = 300
	keeperQuotaWindowUsageCacheTTL = 30 * time.Second
	keeperMaxAuthFileSize          = 10 << 20
	keeperMaxOAuthStateLength      = 4 << 10
	keeperMaxOAuthURLLength        = 32 << 10
	keeperMaxOAuthProjectIDLength  = 256
	keeperFiveHourWindowSeconds    = 5 * 60 * 60
	keeperWeekWindowSeconds        = 7 * 24 * 60 * 60
	keeperMonthWindowSeconds       = 30 * 24 * 60 * 60
)

type KeeperRunner struct {
	app            *App
	mu             sync.Mutex
	daemonStop     chan struct{}
	daemonDone     chan struct{}
	running        bool
	runningModes   map[string]struct{}
	inFlightAuths  map[string]string
	state          string
	detail         string
	mode           *string
	lastStartedAt  *time.Time
	lastFinishedAt *time.Time
	stats          keeperStats
	logs           []string
}

type keeperStats struct {
	Total            int `json:"total"`
	Healthy          int `json:"healthy"`
	StatusDisabled   int `json:"status_disabled"`
	StatusEnabled    int `json:"status_enabled"`
	PriorityDegraded int `json:"priority_degraded"`
	PriorityRestored int `json:"priority_restored"`
	Skipped          int `json:"skipped"`
	NetworkError     int `json:"network_error"`
}

type keeperStatusResponse struct {
	Running        bool        `json:"running"`
	RunningModes   []string    `json:"running_modes"`
	DaemonRunning  bool        `json:"daemon_running"`
	State          string      `json:"state"`
	Detail         string      `json:"detail"`
	Mode           *string     `json:"mode"`
	LastStartedAt  *string     `json:"last_started_at"`
	LastFinishedAt *string     `json:"last_finished_at"`
	Stats          keeperStats `json:"stats"`
	Logs           []string    `json:"logs"`
}

type keeperPriorityRule struct {
	AccountType string `json:"account_type"`
	Priority    int    `json:"priority"`
}

type keeperSettingsUpdateRequest struct {
	ScheduleCron                      *string              `json:"schedule_cron"`
	QuotaThreshold                    *int                 `json:"quota_threshold"`
	UsageTimeoutSeconds               *int                 `json:"usage_timeout_seconds"`
	CPATimeoutSeconds                 *int                 `json:"cpa_timeout_seconds"`
	MaxRetries                        *int                 `json:"max_retries"`
	WorkerThreads                     *int                 `json:"worker_threads"`
	ConditionalRefreshIntervalSeconds *int                 `json:"conditional_refresh_interval_seconds"`
	AccountRefreshCacheMinutes        *int                 `json:"account_refresh_cache_minutes"`
	DryRun                            *bool                `json:"dry_run"`
	EnableCredentialWebsockets        *bool                `json:"enable_credential_websockets"`
	AutoStartDaemon                   *bool                `json:"auto_start_daemon"`
	PriorityRules                     []keeperPriorityRule `json:"priority_rules"`
}

type keeperCronPreviewRequest struct {
	ScheduleCron string `json:"schedule_cron"`
}

type keeperBulkDeleteRequest struct {
	AuthNames []string `json:"auth_names"`
}

type keeperRefreshAccountsRequest struct {
	AuthNames []string `json:"auth_names"`
}

type keeperPriorityUpdateRequest struct {
	Priority int `json:"priority"`
}

type keeperOAuthStartResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

type keeperOAuthStartRequest struct {
	Provider  string `json:"provider"`
	ProjectID string `json:"project_id"`
}

type keeperOAuthStatusResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type keeperOAuthCallbackRequest struct {
	Provider    string `json:"provider"`
	RedirectURL string `json:"redirect_url"`
}

type keeperAccount struct {
	Name                   string         `json:"name"`
	Provider               string         `json:"provider"`
	Email                  *string        `json:"email"`
	Label                  *string        `json:"label"`
	Account                *string        `json:"account"`
	ProjectID              *string        `json:"project_id"`
	UserID                 *string        `json:"user_id"`
	AuthIndex              *string        `json:"auth_index"`
	AccountType            *string        `json:"account_type"`
	PlanType               *string        `json:"plan_type"`
	Weight                 *int           `json:"weight"`
	RequestRetry           *int           `json:"request_retry"`
	Disabled               bool           `json:"disabled"`
	Unavailable            bool           `json:"unavailable"`
	RuntimeOnly            bool           `json:"runtime_only"`
	Status                 *string        `json:"status"`
	StatusMessage          *string        `json:"status_message"`
	Success                *int           `json:"success"`
	Failed                 *int           `json:"failed"`
	Quota                  map[string]any `json:"quota"`
	ModelQuotas            map[string]any `json:"model_quotas"`
	LastRefreshAt          *time.Time     `json:"last_refresh_at"`
	ModifiedAt             *time.Time     `json:"modified_at"`
	Priority               *int           `json:"priority"`
	PrimaryUsedPercent     *int           `json:"primary_used_percent"`
	SecondaryUsedPercent   *int           `json:"secondary_used_percent"`
	PrimaryResetAt         *time.Time     `json:"primary_reset_at"`
	SecondaryResetAt       *time.Time     `json:"secondary_reset_at"`
	PrimaryWindowSeconds   *int           `json:"primary_window_seconds"`
	SecondaryWindowSeconds *int           `json:"secondary_window_seconds"`
	QuotaThreshold         *int           `json:"quota_threshold"`
	LastStatusCode         *int           `json:"last_status_code"`
	LastError              *string        `json:"last_error"`
	LatestAction           *string        `json:"latest_action"`
	LastCheckedAt          *time.Time     `json:"last_checked_at"`
	LastHealthyAt          *time.Time     `json:"last_healthy_at"`
}

type keeperAccountResponse struct {
	Name                   string                          `json:"name"`
	Provider               string                          `json:"provider"`
	Email                  *string                         `json:"email"`
	Label                  *string                         `json:"label"`
	Account                *string                         `json:"account"`
	ProjectID              *string                         `json:"project_id"`
	UserID                 *string                         `json:"user_id"`
	AuthIndex              *string                         `json:"auth_index"`
	AccountType            *string                         `json:"account_type"`
	PlanType               *string                         `json:"plan_type"`
	Weight                 *int                            `json:"weight"`
	RequestRetry           *int                            `json:"request_retry"`
	Disabled               bool                            `json:"disabled"`
	Unavailable            bool                            `json:"unavailable"`
	RuntimeOnly            bool                            `json:"runtime_only"`
	Status                 *string                         `json:"status"`
	StatusMessage          *string                         `json:"status_message"`
	Success                *int                            `json:"success"`
	Failed                 *int                            `json:"failed"`
	Quota                  map[string]any                  `json:"quota"`
	ModelQuotas            map[string]any                  `json:"model_quotas"`
	LastRefreshAt          *string                         `json:"last_refresh_at"`
	ModifiedAt             *string                         `json:"modified_at"`
	Priority               *int                            `json:"priority"`
	PrimaryUsedPercent     *int                            `json:"primary_used_percent"`
	SecondaryUsedPercent   *int                            `json:"secondary_used_percent"`
	PrimaryResetAt         *string                         `json:"primary_reset_at"`
	SecondaryResetAt       *string                         `json:"secondary_reset_at"`
	PrimaryWindowSeconds   *int                            `json:"primary_window_seconds"`
	SecondaryWindowSeconds *int                            `json:"secondary_window_seconds"`
	PrimaryWindowUsage     *keeperQuotaWindowUsageResponse `json:"primary_window_usage"`
	SecondaryWindowUsage   *keeperQuotaWindowUsageResponse `json:"secondary_window_usage"`
	QuotaThreshold         *int                            `json:"quota_threshold"`
	LastStatusCode         *int                            `json:"last_status_code"`
	LastError              *string                         `json:"last_error"`
	LatestAction           *string                         `json:"latest_action"`
	LastCheckedAt          *string                         `json:"last_checked_at"`
	LastHealthyAt          *string                         `json:"last_healthy_at"`
	ResetCredits           *keeperResetCreditsResponse     `json:"reset_credits"`
}

type keeperQuotaWindowUsageResponse struct {
	WindowStart      string  `json:"window_start"`
	WindowEnd        string  `json:"window_end"`
	ResetAt          string  `json:"reset_at"`
	WindowSeconds    int     `json:"window_seconds"`
	Records          int     `json:"records"`
	SuccessRecords   int     `json:"success_records"`
	FailedRecords    int     `json:"failed_records"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CachedTokens     int     `json:"cached_tokens"`
	ReasoningTokens  int     `json:"reasoning_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	UnpricedRecords  int     `json:"unpriced_records"`
	Stale            bool    `json:"stale"`
	WindowSource     string  `json:"window_source"`
}

type keeperQuotaWindowUsage struct {
	WindowStart      time.Time
	WindowEnd        time.Time
	ResetAt          time.Time
	WindowSeconds    int
	Records          int
	SuccessRecords   int
	FailedRecords    int
	InputTokens      int
	OutputTokens     int
	CachedTokens     int
	ReasoningTokens  int
	TotalTokens      int
	EstimatedCostUSD float64
	UnpricedRecords  int
	Stale            bool
	WindowSource     string
}

type keeperQuotaWindowUsagePair struct {
	Primary   *keeperQuotaWindowUsage
	Secondary *keeperQuotaWindowUsage
}

type keeperWindowUsageCache struct {
	mu        sync.Mutex
	key       string
	expiresAt time.Time
	usages    map[string]keeperQuotaWindowUsagePair
}

type keeperResetCreditsResponse struct {
	AvailableCount   int     `json:"available_count"`
	TotalEarnedCount *int    `json:"total_earned_count"`
	EarliestExpires  *string `json:"earliest_expires_at"`
	CachedAt         string  `json:"cached_at"`
}

type keeperResetCreditCache struct {
	mu    sync.RWMutex
	items map[string]keeperResetCreditsResponse
}

type keeperAuthState struct {
	keeperAccount
	RestorePriority *int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type keeperUsageInfo struct {
	PlanType               string
	PrimaryUsedPercent     int
	SecondaryUsedPercent   *int
	PrimaryResetAt         *time.Time
	SecondaryResetAt       *time.Time
	PrimaryWindowSeconds   *int
	SecondaryWindowSeconds *int
}

type keeperHTTPResult struct {
	StatusCode *int
	JSONData   map[string]any
	Brief      string
	Error      string
}

type keeperAccountResult struct {
	Name                   string
	Result                 string
	Email                  *string
	AuthIndex              *string
	AccountType            *string
	Priority               *int
	RestorePriority        *int
	ClearRestorePriority   bool
	Disabled               *bool
	PrimaryUsedPercent     *int
	SecondaryUsedPercent   *int
	PrimaryResetAt         *time.Time
	SecondaryResetAt       *time.Time
	PrimaryWindowSeconds   *int
	SecondaryWindowSeconds *int
	QuotaThreshold         *int
	LastStatusCode         *int
	LastError              *string
	LatestAction           *string
	CheckedAt              time.Time
}

func NewKeeperRunner(app *App) *KeeperRunner {
	return &KeeperRunner{
		app:    app,
		state:  "idle",
		detail: "尚未运行",
		logs:   []string{},
	}
}

func (r *KeeperRunner) LoadPersistedState(ctx context.Context) {
	logs, logErr := r.app.loadKeeperLogLines(keeperMaxInMemoryLogs)
	if logErr != nil {
		log.Printf("restore codex keeper logs failed: %v", logErr)
	}
	run, err := r.app.latestKeeperRun(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = logs
	if err != nil || run == nil {
		return
	}
	r.state = run.State
	r.detail = run.Detail
	r.mode = run.Mode
	r.lastStartedAt = run.StartedAt
	r.lastFinishedAt = run.FinishedAt
	r.stats = run.Stats
}

func (r *KeeperRunner) StartAutoIfConfigured() {
	cfg, err := r.app.loadConfig(context.Background())
	if err != nil {
		r.log("读取 Codex Keeper 自动启动配置失败：" + err.Error())
		return
	}
	if cfg.CodexKeeper.AutoStartDaemon && strings.TrimSpace(cfg.Collector.ManagementKey) != "" {
		if err := r.StartDaemon(); err != nil {
			r.log("启动 Codex Keeper 自动巡检失败：" + err.Error())
		}
	}
}

func (r *KeeperRunner) StartOnce() error {
	if !r.markRunning("once") {
		return conflictError("Codex Keeper 正在运行")
	}
	go r.run("once")
	return nil
}

func (r *KeeperRunner) StartAccounts(authNames []string) error {
	names, err := normalizeKeeperAuthNames(authNames)
	if err != nil {
		return err
	}
	if !r.markRunning("accounts") {
		return conflictError("Codex Keeper 正在运行")
	}
	go r.runAccounts("accounts", names)
	return nil
}

func (r *KeeperRunner) StartDaemon() error {
	cfg, err := r.app.loadConfig(context.Background())
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return validationError("管理密钥未设置，无法运行 Codex Keeper")
	}
	if _, _, err := nextRunTimes(cfg.CodexKeeper.ScheduleCron, 1, time.Now()); err != nil {
		return err
	}

	r.mu.Lock()
	if r.daemonRunningLocked() {
		r.mu.Unlock()
		return nil
	}
	r.daemonStop = make(chan struct{})
	r.daemonDone = make(chan struct{})
	stop := r.daemonStop
	done := r.daemonDone
	r.mu.Unlock()

	go r.daemonLoop(stop, done)
	r.log("Codex Keeper 已开始按计划自动巡检")
	return nil
}

func (r *KeeperRunner) Stop() {
	r.mu.Lock()
	stop := r.daemonStop
	done := r.daemonDone
	if stop == nil || done == nil {
		r.mu.Unlock()
		return
	}
	select {
	case <-done:
		r.daemonStop = nil
		r.daemonDone = nil
		r.mu.Unlock()
		return
	default:
	}
	select {
	case <-stop:
	default:
		close(stop)
	}
	r.mu.Unlock()
	<-done
	r.mu.Lock()
	if r.daemonStop == stop {
		r.daemonStop = nil
	}
	if r.daemonDone == done {
		r.daemonDone = nil
	}
	r.mu.Unlock()
	r.log("Codex Keeper 已停止自动巡检")
}

func (r *KeeperRunner) ClearLogs() {
	r.mu.Lock()
	r.logs = []string{}
	r.mu.Unlock()
	if err := r.app.clearKeeperLogFiles(); err != nil {
		log.Printf("clear codex keeper log files failed: %v", err)
	}
}

func (r *KeeperRunner) Status() keeperStatusResponse {
	r.mu.Lock()
	logs := append([]string{}, r.logs...)
	runningModes := r.runningModeListLocked()
	response := keeperStatusResponse{
		Running:        len(runningModes) > 0,
		RunningModes:   runningModes,
		DaemonRunning:  r.daemonRunningLocked(),
		State:          r.state,
		Detail:         r.detail,
		Mode:           cloneStringPtr(r.mode),
		LastStartedAt:  apiDateTimePtr(r.lastStartedAt),
		LastFinishedAt: apiDateTimePtr(r.lastFinishedAt),
		Stats:          r.stats,
		Logs:           logs,
	}
	r.mu.Unlock()
	if r.app != nil {
		response.Stats = keeperStats{}
		if run, err := r.app.latestKeeperRunByMode(context.Background(), "daemon"); err == nil && run != nil {
			response.Stats = run.Stats
		}
	}
	return response
}

func (r *KeeperRunner) daemonRunningLocked() bool {
	if r.daemonDone == nil {
		return false
	}
	select {
	case <-r.daemonDone:
		return false
	default:
		return true
	}
}

func (r *KeeperRunner) daemonLoop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		cfg, err := r.app.loadConfig(context.Background())
		if err != nil {
			r.log("读取 Codex Keeper 配置失败：" + err.Error())
			if waitForStop(stop, time.Minute) {
				return
			}
			continue
		}
		times, _, err := nextRunTimes(cfg.CodexKeeper.ScheduleCron, 1, time.Now().In(appTimeLocation))
		if err != nil {
			r.log("Codex Keeper 定时表达式无效：" + err.Error())
			if waitForStop(stop, time.Minute) {
				return
			}
			continue
		}
		delay := positiveDuration(time.Until(times[0]))
		r.log("下一轮计划：" + times[0].In(appTimeLocation).Format("2006-01-02 15:04:05"))
		cronTimer := time.NewTimer(delay)
		conditionalInterval := keeperConditionalRefreshInterval(cfg)
		var conditionalTicker *time.Ticker
		var conditionalC <-chan time.Time
		if conditionalInterval > 0 {
			conditionalTicker = time.NewTicker(conditionalInterval)
			conditionalC = conditionalTicker.C
		}

		restartCycle := false
		for !restartCycle {
			select {
			case <-stop:
				cronTimer.Stop()
				if conditionalTicker != nil {
					conditionalTicker.Stop()
				}
				return
			case <-cronTimer.C:
				if conditionalTicker != nil {
					conditionalTicker.Stop()
				}
				if r.markRunning("daemon") {
					go r.run("daemon")
				}
				restartCycle = true
			case <-conditionalC:
				nextCfg, err := r.app.loadConfig(context.Background())
				if err != nil {
					r.log("读取 Codex Keeper 条件刷新配置失败：" + err.Error())
					continue
				}
				nextInterval := keeperConditionalRefreshInterval(nextCfg)
				if nextInterval != conditionalInterval {
					cronTimer.Stop()
					if conditionalTicker != nil {
						conditionalTicker.Stop()
					}
					restartCycle = true
					continue
				}
				names, err := r.app.conditionalKeeperRefreshCandidates(context.Background(), nextCfg)
				if err != nil {
					r.log("Codex Keeper 条件刷新候选查询失败：" + err.Error())
					continue
				}
				if len(names) == 0 {
					continue
				}
				if r.markRunning("conditional") {
					go r.runAccounts("conditional", names)
				}
			}
		}
	}
}

func positiveDuration(delay time.Duration) time.Duration {
	if delay < 0 {
		return 0
	}
	return delay
}

func keeperConditionalRefreshInterval(cfg AppConfig) time.Duration {
	seconds := cfg.CodexKeeper.ConditionalRefreshIntervalSeconds
	if !validKeeperConditionalRefreshInterval(seconds) || seconds == 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (r *KeeperRunner) ensureRunningModesLocked() {
	if r.runningModes == nil {
		r.runningModes = map[string]struct{}{}
	}
}

func (r *KeeperRunner) runningModeListLocked() []string {
	r.ensureRunningModesLocked()
	modes := make([]string, 0, len(r.runningModes))
	for mode := range r.runningModes {
		modes = append(modes, mode)
	}
	sort.Slice(modes, func(i, j int) bool {
		leftOrder := keeperModeOrder(modes[i])
		rightOrder := keeperModeOrder(modes[j])
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return modes[i] < modes[j]
	})
	return modes
}

func (r *KeeperRunner) ensureInFlightAuthsLocked() {
	if r.inFlightAuths == nil {
		r.inFlightAuths = map[string]string{}
	}
}

func (r *KeeperRunner) tryLockAuthName(mode string, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInFlightAuthsLocked()
	if _, exists := r.inFlightAuths[name]; exists {
		return false
	}
	r.inFlightAuths[name] = mode
	return true
}

func (r *KeeperRunner) unlockAuthName(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inFlightAuths == nil {
		return
	}
	delete(r.inFlightAuths, name)
}

func keeperModeOrder(mode string) int {
	switch mode {
	case "daemon":
		return 0
	case "once":
		return 1
	case "conditional":
		return 2
	case "accounts":
		return 3
	default:
		return 99
	}
}

func keeperModesConflict(existingMode string, nextMode string) bool {
	if existingMode == nextMode {
		return true
	}
	return (existingMode == "once" && nextMode == "daemon") ||
		(existingMode == "daemon" && nextMode == "once")
}

func keeperStatusModePtr(modes []string) *string {
	if len(modes) == 0 {
		return nil
	}
	mode := modes[0]
	return &mode
}

func keeperRunningDetail(modes []string) string {
	if len(modes) > 1 {
		return "正在运行多个 Codex Keeper 任务"
	}
	if len(modes) == 0 {
		return "尚未运行"
	}
	switch modes[0] {
	case "accounts":
		return "正在刷新 Codex 凭证"
	case "conditional":
		return "正在按条件刷新 Codex 凭证"
	default:
		return "正在巡检 Codex 凭证"
	}
}

func (r *KeeperRunner) markRunning(mode string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureRunningModesLocked()
	for runningMode := range r.runningModes {
		if keeperModesConflict(runningMode, mode) {
			return false
		}
	}
	r.runningModes[mode] = struct{}{}
	now := time.Now().In(appTimeLocation)
	r.running = true
	r.state = "running"
	runningModes := r.runningModeListLocked()
	r.detail = keeperRunningDetail(runningModes)
	r.mode = keeperStatusModePtr(runningModes)
	r.lastStartedAt = &now
	r.lastFinishedAt = nil
	r.stats = keeperStats{}
	return true
}

func (r *KeeperRunner) run(mode string) {
	r.runAccounts(mode, nil)
}

func (r *KeeperRunner) runAccounts(mode string, authNames []string) {
	options := keeperRunOptionsForMode(mode, authNames)
	options.TryLockAuthName = r.tryLockAuthName
	options.UnlockAuthName = r.unlockAuthName
	stats, detail, err := r.app.executeKeeperRunWithOptions(context.Background(), options, r.log)
	finishedAt := time.Now().In(appTimeLocation)
	logMessage := detail
	if err != nil {
		logMessage = "巡检失败：" + err.Error()
	}
	r.mu.Lock()
	r.ensureRunningModesLocked()
	delete(r.runningModes, mode)
	runningModes := r.runningModeListLocked()
	r.running = len(runningModes) > 0
	r.lastFinishedAt = &finishedAt
	r.stats = stats
	if r.running {
		r.state = "running"
		r.detail = keeperRunningDetail(runningModes)
		r.mode = keeperStatusModePtr(runningModes)
	} else if err != nil {
		r.state = "failed"
		r.detail = err.Error()
	} else {
		completedMode := mode
		r.mode = &completedMode
		r.state = "completed"
		r.detail = detail
	}
	r.mu.Unlock()
	if strings.TrimSpace(logMessage) != "" {
		r.log(logMessage)
	}
}

func (r *KeeperRunner) log(message string) {
	timestamp := time.Now().In(appTimeLocation)
	line := formatKeeperLogLine(timestamp, message)
	r.mu.Lock()
	r.logs = appendKeeperLog(r.logs, line)
	r.mu.Unlock()
	if err := r.app.appendKeeperLogFile(timestamp, line); err != nil {
		log.Printf("write codex keeper log failed: %v", err)
	}
}

func appendKeeperLog(logs []string, line string) []string {
	logs = append(logs, line)
	if len(logs) > keeperMaxInMemoryLogs {
		logs = logs[len(logs)-keeperMaxInMemoryLogs:]
	}
	return logs
}

func formatKeeperLogLine(timestamp time.Time, message string) string {
	var output strings.Builder
	handler := slog.NewTextHandler(&output, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && attr.Key == slog.TimeKey {
				return slog.String(slog.TimeKey, timestamp.In(appTimeLocation).Format("2006-01-02T15:04:05.000Z07:00"))
			}
			return attr
		},
	})
	record := slog.NewRecord(timestamp.In(appTimeLocation), slog.LevelInfo, message, 0)
	record.AddAttrs(slog.String("component", keeperLogComponent))
	_ = handler.Handle(context.Background(), record)
	return strings.TrimSuffix(output.String(), "\n")
}

type keeperLogFile struct {
	path string
	date time.Time
}

func (a *App) keeperLogDir() string {
	return filepath.Join(a.dataDir, "logs")
}

func (a *App) appendKeeperLogFile(timestamp time.Time, line string) error {
	dir := a.keeperLogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, keeperLogFilePrefix+timestamp.In(appTimeLocation).Format("2006-01-02")+".log")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(line + "\n")
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return a.pruneKeeperLogFiles()
}

func (a *App) loadKeeperLogLines(limit int) ([]string, error) {
	files, err := a.keeperLogFiles()
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].date.Before(files[j].date)
	})
	if len(files) > keeperLogRetainedFiles {
		files = files[len(files)-keeperLogRetainedFiles:]
	}
	lines := []string{}
	for _, file := range files {
		handle, err := os.Open(file.path)
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(handle)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			lines = appendKeeperLog(lines, line)
		}
		scanErr := scanner.Err()
		closeErr := handle.Close()
		if scanErr != nil {
			return nil, scanErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	if limit > 0 && len(lines) > limit {
		return lines[len(lines)-limit:], nil
	}
	return lines, nil
}

func (a *App) pruneKeeperLogFiles() error {
	files, err := a.keeperLogFiles()
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].date.After(files[j].date)
	})
	for index, file := range files {
		if index < keeperLogRetainedFiles {
			continue
		}
		if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (a *App) clearKeeperLogFiles() error {
	files, err := a.keeperLogFiles()
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (a *App) keeperLogFiles() ([]keeperLogFile, error) {
	dir := a.keeperLogDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := []keeperLogFile{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, keeperLogFilePrefix) || !strings.HasSuffix(name, ".log") {
			continue
		}
		dateText := strings.TrimSuffix(strings.TrimPrefix(name, keeperLogFilePrefix), ".log")
		date, err := time.ParseInLocation("2006-01-02", dateText, appTimeLocation)
		if err != nil {
			continue
		}
		files = append(files, keeperLogFile{
			path: filepath.Join(dir, name),
			date: date,
		})
	}
	return files, nil
}

func (a *App) handleCodexKeeper(w http.ResponseWriter, r *http.Request) error {
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}
	parts := splitPath(r.URL.Path, "/api/codex-keeper/")
	if len(parts) == 0 {
		return notFoundError("Not Found")
	}
	readOnlyStatusRequest := r.Method == http.MethodGet && len(parts) == 1 && (parts[0] == "status" || parts[0] == "accounts")
	if !user.IsAdmin && (!user.CanViewAccountStatus || !readOnlyStatusRequest) {
		return forbiddenError("需要管理员权限")
	}
	switch {
	case len(parts) == 1 && parts[0] == "settings":
		if r.Method == http.MethodGet {
			cfg, err := a.loadConfig(r.Context())
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, keeperSettingsResponse(cfg))
			return nil
		}
		if r.Method == http.MethodPut {
			return a.updateKeeperSettings(w, r)
		}
		return methodNotAllowed()
	case len(parts) == 2 && parts[0] == "schedule" && parts[1] == "preview":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		var payload keeperCronPreviewRequest
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		times, normalized, err := nextRunTimes(payload.ScheduleCron, 5, time.Now())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"schedule_cron": normalized, "next_run_times": apiDateTimes(times)})
		return nil
	case len(parts) == 1 && parts[0] == "status":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, a.keeper.Status())
		return nil
	case len(parts) == 1 && parts[0] == "accounts":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		accounts, degraded, err := a.listKeeperCredentials(r.Context())
		if err != nil {
			return err
		}
		windowUsages, err := a.keeperQuotaWindowUsages(r.Context(), accounts)
		if err != nil {
			return err
		}
		cfg, err := a.loadConfig(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items":          a.keeperAccountResponses(accounts, windowUsages),
			"priority_rules": sortedPriorityRules(cfg.CodexKeeperPriorityRule),
			"degraded":       degraded,
		})
		return nil
	case len(parts) == 3 && parts[0] == "auth-files" && parts[2] == "models":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		authName, err := keeperAccountNameFromPath(parts[1])
		if err != nil {
			return err
		}
		cfg, err := a.loadConfig(r.Context())
		if err != nil {
			return err
		}
		models, err := a.listKeeperRemoteAuthFileModels(r.Context(), cfg, authName)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"models": models})
		return nil
	case len(parts) == 2 && parts[0] == "oauth" && parts[1] == "start":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		return a.startKeeperOAuth(w, r)
	case len(parts) == 2 && parts[0] == "oauth" && parts[1] == "status":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		return a.getKeeperOAuthStatus(w, r)
	case len(parts) == 2 && parts[0] == "oauth" && parts[1] == "callback":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		return a.submitKeeperOAuthCallback(w, r)
	case len(parts) == 1 && parts[0] == "auth-files":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		return a.uploadKeeperAuthFiles(w, r)
	case len(parts) == 2 && parts[0] == "auth-files":
		authName, err := url.PathUnescape(parts[1])
		if err != nil || strings.TrimSpace(authName) == "" || strings.ContainsAny(authName, `/\\`) {
			return validationError("认证文件名称无效")
		}
		cfg, err := a.loadConfig(r.Context())
		if err != nil {
			return err
		}
		switch r.Method {
		case http.MethodGet:
			detail, err := a.getKeeperRemoteAuthFileEditorDetail(r.Context(), cfg, authName)
			if err != nil {
				return err
			}
			if detail == nil {
				return notFoundError("认证文件不存在")
			}
			writeJSON(w, http.StatusOK, detail)
			return nil
		case http.MethodPatch:
			var fields map[string]any
			if err := decodeJSON(r, &fields); err != nil {
				return err
			}
			if err := a.updateKeeperRemoteAuthFileFields(r.Context(), cfg, authName, fields); err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
			return nil
		default:
			return methodNotAllowed()
		}
	case len(parts) == 1 && parts[0] == "run-once":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		if err := a.keeper.StartOnce(); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
		return nil
	case len(parts) == 1 && parts[0] == "start":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		if err := a.keeper.StartDaemon(); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
		return nil
	case len(parts) == 1 && parts[0] == "stop":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		a.keeper.Stop()
		writeJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
		return nil
	case len(parts) == 2 && parts[0] == "logs" && parts[1] == "clear":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		a.keeper.ClearLogs()
		writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
		return nil
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "bulk-delete":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		return a.bulkDeleteKeeperAccounts(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "refresh":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		var payload keeperRefreshAccountsRequest
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		if err := a.keeper.StartAccounts(payload.AuthNames); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
		return nil
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "sync":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		added, removed, err := a.syncKeeperAccountList(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "synced",
			"added":   added,
			"removed": removed,
		})
		return nil
	case len(parts) == 3 && parts[0] == "accounts" && (parts[2] == "enable" || parts[2] == "disable"):
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		authName, err := url.PathUnescape(parts[1])
		if err != nil {
			return validationError("凭证名称无效")
		}
		disabled := parts[2] == "disable"
		if err := a.setKeeperAccountDisabled(r.Context(), authName, disabled); err != nil {
			return err
		}
		if disabled {
			writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
		} else {
			writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
		}
		return nil
	case len(parts) == 2 && parts[0] == "accounts" && r.Method == http.MethodDelete:
		authName, err := url.PathUnescape(parts[1])
		if err != nil {
			return validationError("凭证名称无效")
		}
		if err := a.deleteKeeperAccount(r.Context(), authName); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return nil
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "priority":
		if err := requireMethod(r, http.MethodPatch); err != nil {
			return err
		}
		authName, err := url.PathUnescape(parts[1])
		if err != nil {
			return validationError("凭证名称无效")
		}
		var payload keeperPriorityUpdateRequest
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		if err := a.updateKeeperAccountPriority(r.Context(), authName, payload.Priority); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return nil
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "reset-credits" && parts[3] == "query":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		authName, err := keeperAccountNameFromPath(parts[1])
		if err != nil {
			return err
		}
		result, err := a.queryKeeperResetCredits(r.Context(), authName)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, result)
		return nil
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "reset-credits" && parts[3] == "consume":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		authName, err := keeperAccountNameFromPath(parts[1])
		if err != nil {
			return err
		}
		result, err := a.consumeKeeperResetCredit(r.Context(), authName)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, result)
		return nil
	default:
		return notFoundError("Not Found")
	}
}

func keeperAccountNameFromPath(value string) (string, error) {
	authName, err := url.PathUnescape(value)
	if err != nil || strings.TrimSpace(authName) == "" || strings.ContainsAny(authName, `/\\`) {
		return "", validationError("凭证名称无效")
	}
	return authName, nil
}

func keeperSettingsResponse(cfg AppConfig) map[string]any {
	times, normalized, err := nextRunTimes(cfg.CodexKeeper.ScheduleCron, 5, time.Now())
	if err != nil {
		normalized = cfg.CodexKeeper.ScheduleCron
		times = []time.Time{}
	}
	return map[string]any{
		"cliaproxy_url":                        cfg.Collector.CLIProxyURL,
		"management_key_set":                   strings.TrimSpace(cfg.Collector.ManagementKey) != "",
		"schedule_cron":                        normalized,
		"next_run_times":                       apiDateTimes(times),
		"quota_threshold":                      cfg.CodexKeeper.QuotaThreshold,
		"usage_timeout_seconds":                cfg.CodexKeeper.UsageTimeoutSeconds,
		"cpa_timeout_seconds":                  cfg.CodexKeeper.CPATimeoutSeconds,
		"max_retries":                          cfg.CodexKeeper.MaxRetries,
		"worker_threads":                       cfg.CodexKeeper.WorkerThreads,
		"conditional_refresh_interval_seconds": cfg.CodexKeeper.ConditionalRefreshIntervalSeconds,
		"account_refresh_cache_minutes":        cfg.CodexKeeper.AccountRefreshCacheMinutes,
		"dry_run":                              cfg.CodexKeeper.DryRun,
		"enable_credential_websockets":         cfg.CodexKeeper.EnableCredentialWebsockets,
		"auto_start_daemon":                    cfg.CodexKeeper.AutoStartDaemon,
		"priority_rules":                       sortedPriorityRules(cfg.CodexKeeperPriorityRule),
	}
}

func (a *App) keeperAccountResponses(accounts []keeperAccount, windowUsages map[string]keeperQuotaWindowUsagePair) []keeperAccountResponse {
	responses := make([]keeperAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		usage := windowUsages[account.Name]
		responses = append(responses, keeperAccountResponse{
			Name:                   account.Name,
			Provider:               account.Provider,
			Email:                  account.Email,
			Label:                  account.Label,
			Account:                account.Account,
			ProjectID:              account.ProjectID,
			UserID:                 account.UserID,
			AuthIndex:              account.AuthIndex,
			AccountType:            account.AccountType,
			PlanType:               account.PlanType,
			Weight:                 account.Weight,
			RequestRetry:           account.RequestRetry,
			Disabled:               account.Disabled,
			Unavailable:            account.Unavailable,
			RuntimeOnly:            account.RuntimeOnly,
			Status:                 account.Status,
			StatusMessage:          account.StatusMessage,
			Success:                account.Success,
			Failed:                 account.Failed,
			Quota:                  account.Quota,
			ModelQuotas:            account.ModelQuotas,
			LastRefreshAt:          apiDateTimePtr(account.LastRefreshAt),
			ModifiedAt:             apiDateTimePtr(account.ModifiedAt),
			Priority:               keeperDisplayPriority(account.Priority),
			PrimaryUsedPercent:     account.PrimaryUsedPercent,
			SecondaryUsedPercent:   account.SecondaryUsedPercent,
			PrimaryResetAt:         apiDateTimePtr(account.PrimaryResetAt),
			SecondaryResetAt:       apiDateTimePtr(account.SecondaryResetAt),
			PrimaryWindowSeconds:   account.PrimaryWindowSeconds,
			SecondaryWindowSeconds: account.SecondaryWindowSeconds,
			PrimaryWindowUsage:     keeperQuotaWindowUsageResponseFrom(usage.Primary),
			SecondaryWindowUsage:   keeperQuotaWindowUsageResponseFrom(usage.Secondary),
			QuotaThreshold:         account.QuotaThreshold,
			LastStatusCode:         account.LastStatusCode,
			LastError:              account.LastError,
			LatestAction:           account.LatestAction,
			LastCheckedAt:          apiDateTimePtr(account.LastCheckedAt),
			LastHealthyAt:          apiDateTimePtr(account.LastHealthyAt),
			ResetCredits:           keeperResetCreditsForAccount(a, account),
		})
	}
	return responses
}

func keeperResetCreditsForAccount(a *App, account keeperAccount) *keeperResetCreditsResponse {
	if a == nil || !keeperIsCodexProvider(account.Provider) {
		return nil
	}
	return a.cachedKeeperResetCredits(account.Name)
}

func keeperQuotaWindowUsageResponseFrom(usage *keeperQuotaWindowUsage) *keeperQuotaWindowUsageResponse {
	if usage == nil {
		return nil
	}
	return &keeperQuotaWindowUsageResponse{
		WindowStart:      apiDateTime(usage.WindowStart),
		WindowEnd:        apiDateTime(usage.WindowEnd),
		ResetAt:          apiDateTime(usage.ResetAt),
		WindowSeconds:    usage.WindowSeconds,
		Records:          usage.Records,
		SuccessRecords:   usage.SuccessRecords,
		FailedRecords:    usage.FailedRecords,
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CachedTokens:     usage.CachedTokens,
		ReasoningTokens:  usage.ReasoningTokens,
		TotalTokens:      usage.TotalTokens,
		EstimatedCostUSD: usage.EstimatedCostUSD,
		UnpricedRecords:  usage.UnpricedRecords,
		Stale:            usage.Stale,
		WindowSource:     usage.WindowSource,
	}
}

func (a *App) cachedKeeperResetCredits(authName string) *keeperResetCreditsResponse {
	a.keeperResetCache.mu.RLock()
	defer a.keeperResetCache.mu.RUnlock()
	result, ok := a.keeperResetCache.items[authName]
	if !ok {
		return nil
	}
	copy := result
	return &copy
}

func (a *App) storeKeeperResetCredits(authName string, result keeperResetCreditsResponse) {
	a.keeperResetCache.mu.Lock()
	defer a.keeperResetCache.mu.Unlock()
	if a.keeperResetCache.items == nil {
		a.keeperResetCache.items = map[string]keeperResetCreditsResponse{}
	}
	a.keeperResetCache.items[authName] = result
}

func (a *App) deleteCachedKeeperResetCredits(authName string) {
	a.keeperResetCache.mu.Lock()
	defer a.keeperResetCache.mu.Unlock()
	delete(a.keeperResetCache.items, authName)
}

func (a *App) keeperQuotaWindowUsages(ctx context.Context, accounts []keeperAccount) (map[string]keeperQuotaWindowUsagePair, error) {
	if len(accounts) == 0 {
		return map[string]keeperQuotaWindowUsagePair{}, nil
	}
	key, err := a.keeperQuotaWindowUsageCacheKey(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().In(appTimeLocation)
	if cached, ok := a.cachedKeeperQuotaWindowUsages(key, now); ok {
		return cached, nil
	}
	usages, err := a.computeKeeperQuotaWindowUsages(ctx, accounts, now)
	if err != nil {
		return nil, err
	}
	a.storeKeeperQuotaWindowUsages(key, now.Add(keeperQuotaWindowUsageCacheTTL), usages)
	return usages, nil
}

func (a *App) keeperQuotaWindowUsageCacheKey(ctx context.Context) (string, error) {
	var stateUpdated, usageID sql.NullString
	if err := a.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(CAST(updated_at AS TEXT)), '') FROM codex_keeper_auth_states`).Scan(&stateUpdated); err != nil {
		return "", err
	}
	if err := a.db.QueryRowContext(ctx, `SELECT COALESCE(CAST(MAX(id) AS TEXT), '') FROM usage_records`).Scan(&usageID); err != nil {
		return "", err
	}
	return stateUpdated.String + "|" + usageID.String, nil
}

func (a *App) cachedKeeperQuotaWindowUsages(key string, now time.Time) (map[string]keeperQuotaWindowUsagePair, bool) {
	a.keeperUsageCache.mu.Lock()
	defer a.keeperUsageCache.mu.Unlock()
	if key == "" || key != a.keeperUsageCache.key || !now.Before(a.keeperUsageCache.expiresAt) {
		return nil, false
	}
	return copyKeeperQuotaWindowUsagePairs(a.keeperUsageCache.usages), true
}

func (a *App) storeKeeperQuotaWindowUsages(key string, expiresAt time.Time, usages map[string]keeperQuotaWindowUsagePair) {
	a.keeperUsageCache.mu.Lock()
	defer a.keeperUsageCache.mu.Unlock()
	a.keeperUsageCache.key = key
	a.keeperUsageCache.expiresAt = expiresAt
	a.keeperUsageCache.usages = copyKeeperQuotaWindowUsagePairs(usages)
}

func copyKeeperQuotaWindowUsagePairs(source map[string]keeperQuotaWindowUsagePair) map[string]keeperQuotaWindowUsagePair {
	if source == nil {
		return map[string]keeperQuotaWindowUsagePair{}
	}
	copied := make(map[string]keeperQuotaWindowUsagePair, len(source))
	for name, pair := range source {
		copied[name] = keeperQuotaWindowUsagePair{
			Primary:   copyKeeperQuotaWindowUsage(pair.Primary),
			Secondary: copyKeeperQuotaWindowUsage(pair.Secondary),
		}
	}
	return copied
}

func copyKeeperQuotaWindowUsage(source *keeperQuotaWindowUsage) *keeperQuotaWindowUsage {
	if source == nil {
		return nil
	}
	copied := *source
	return &copied
}

func (a *App) computeKeeperQuotaWindowUsages(ctx context.Context, accounts []keeperAccount, now time.Time) (map[string]keeperQuotaWindowUsagePair, error) {
	usages := map[string]keeperQuotaWindowUsagePair{}
	sourceAccounts := map[string]string{}
	aliases := map[string][]string{}
	var minStart, maxEnd time.Time
	maxWindowSeconds := 0

	for _, account := range accounts {
		addKeeperAuthAlias(aliases, account.Name, account.Name)
		addKeeperSourceAccountAlias(sourceAccounts, account.Name, account.Name)
		if account.AuthIndex != nil {
			addKeeperAuthAlias(aliases, *account.AuthIndex, account.Name)
		}
		if account.Email != nil {
			addKeeperAuthAlias(aliases, *account.Email, account.Name)
			addKeeperSourceAccountAlias(sourceAccounts, *account.Email, account.Name)
		}
		if account.Disabled {
			continue
		}

		pair := keeperQuotaWindowPairForAccount(account, now)
		if pair.Primary == nil && pair.Secondary == nil {
			continue
		}
		usages[account.Name] = pair
		minStart, maxEnd = keeperQuotaWindowBounds(minStart, maxEnd, pair.Primary)
		minStart, maxEnd = keeperQuotaWindowBounds(minStart, maxEnd, pair.Secondary)
		for _, usage := range []*keeperQuotaWindowUsage{pair.Primary, pair.Secondary} {
			if usage != nil && usage.WindowSeconds > maxWindowSeconds {
				maxWindowSeconds = usage.WindowSeconds
			}
		}
	}
	if minStart.IsZero() || maxEnd.IsZero() {
		return usages, nil
	}

	queryStart := minStart
	if maxWindowSeconds > 0 {
		maxLookbackStart := now.Add(-time.Duration(maxWindowSeconds) * time.Second)
		if queryStart.Before(maxLookbackStart) {
			queryStart = maxLookbackStart
		}
	}
	records, err := a.keeperUsageRecordsInRange(ctx, queryStart, maxEnd)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		accountName, ok := keeperAccountNameForUsageRecord(record, sourceAccounts, aliases)
		if !ok {
			continue
		}
		pair, ok := usages[accountName]
		if !ok {
			continue
		}
		if keeperRecordInQuotaWindow(record, pair.Primary) {
			addRecordToKeeperQuotaWindowUsage(pair.Primary, record, nil)
		}
		if keeperRecordInQuotaWindow(record, pair.Secondary) {
			addRecordToKeeperQuotaWindowUsage(pair.Secondary, record, nil)
		}
	}
	return usages, nil
}

func keeperQuotaWindowPairForAccount(account keeperAccount, now time.Time) keeperQuotaWindowUsagePair {
	return keeperQuotaWindowUsagePair{
		Primary:   keeperQuotaWindowForAccount(account, true, now),
		Secondary: keeperQuotaWindowForAccount(account, false, now),
	}
}

func keeperQuotaWindowForAccount(account keeperAccount, primary bool, now time.Time) *keeperQuotaWindowUsage {
	planType := keeperQuotaPlanType(account)
	if !primary && keeperFreeQuotaWindowAccount(planType) {
		return nil
	}
	resetAt := account.PrimaryResetAt
	windowSeconds := account.PrimaryWindowSeconds
	if !primary {
		resetAt = account.SecondaryResetAt
		windowSeconds = account.SecondaryWindowSeconds
	}
	if resetAt == nil {
		return nil
	}
	seconds, source, ok := keeperQuotaWindowSeconds(planType, windowSeconds, primary)
	if primary && windowSeconds == nil && keeperProQuotaWindowAccount(planType) && account.SecondaryUsedPercent == nil {
		seconds, source, ok = keeperWeekWindowSeconds, "inferred", true
	}
	if !ok {
		return nil
	}
	windowEnd := resetAt.In(appTimeLocation)
	windowStart := windowEnd.Add(-time.Duration(seconds) * time.Second)
	return &keeperQuotaWindowUsage{
		WindowStart:   windowStart,
		WindowEnd:     windowEnd,
		ResetAt:       windowEnd,
		WindowSeconds: seconds,
		Stale:         !now.Before(windowEnd),
		WindowSource:  source,
	}
}

func keeperQuotaPlanType(account keeperAccount) *string {
	if account.PlanType != nil && strings.TrimSpace(*account.PlanType) != "" {
		return account.PlanType
	}
	return account.AccountType
}

func keeperQuotaWindowSeconds(accountType *string, saved *int, primary bool) (int, string, bool) {
	if saved != nil && *saved > 0 {
		return *saved, "codex", true
	}
	if keeperFreeQuotaWindowAccount(accountType) {
		return keeperMonthWindowSeconds, "inferred", true
	}
	if keeperPaidQuotaWindowAccount(accountType) {
		if primary {
			return keeperFiveHourWindowSeconds, "inferred", true
		}
		return keeperWeekWindowSeconds, "inferred", true
	}
	return 0, "", false
}

func keeperFreeQuotaWindowAccount(accountType *string) bool {
	return strings.ToLower(strings.TrimSpace(valueOr(accountType, ""))) == "free"
}

func keeperPaidQuotaWindowAccount(accountType *string) bool {
	normalized := strings.ToLower(strings.TrimSpace(valueOr(accountType, "")))
	return normalized == "plus" || normalized == "team" || normalized == "k12" || strings.HasPrefix(normalized, "pro")
}

func keeperProQuotaWindowAccount(accountType *string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(valueOr(accountType, ""))), "pro")
}

func keeperQuotaWindowBounds(minStart, maxEnd time.Time, usage *keeperQuotaWindowUsage) (time.Time, time.Time) {
	if usage == nil {
		return minStart, maxEnd
	}
	if minStart.IsZero() || usage.WindowStart.Before(minStart) {
		minStart = usage.WindowStart
	}
	if maxEnd.IsZero() || usage.WindowEnd.After(maxEnd) {
		maxEnd = usage.WindowEnd
	}
	return minStart, maxEnd
}

func (a *App) keeperUsageRecordsInRange(ctx context.Context, start, end time.Time) ([]UsageRecord, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id, CAST(timestamp AS TEXT), usage_username, api_key_description, provider, model, reasoning_effort, request_service_tier, endpoint, source,
		source_account, request_id, auth, auth_index, latency_ms, ttft_ms, failed, input_tokens, output_tokens, cached_tokens,
		cache_read_tokens, cache_creation_tokens, reasoning_tokens, total_tokens, cost_usd, unpriced, dedupe_key, raw_json
		FROM usage_records
		WHERE timestamp >= ? AND timestamp < ?
		ORDER BY timestamp`, dbTime(start), dbTime(end))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsageRecords(rows)
}

func addKeeperSourceAccountAlias(aliases map[string]string, value string, name string) {
	valuePtr := &value
	sourceAccount := sourceAccountFromUsageSource(valuePtr)
	if sourceAccount == nil {
		return
	}
	normalizedName := strings.TrimSpace(name)
	if normalizedName == "" {
		return
	}
	existing, ok := aliases[*sourceAccount]
	if ok && existing != normalizedName {
		aliases[*sourceAccount] = ""
		return
	}
	aliases[*sourceAccount] = normalizedName
}

func keeperAccountNameForUsageRecord(record UsageRecord, sourceAccounts map[string]string, aliases map[string][]string) (string, bool) {
	if sourceAccount := keeperUsageRecordSourceAccount(record); sourceAccount != "" {
		if name, ok := sourceAccounts[sourceAccount]; ok {
			if name != "" {
				return name, true
			}
		} else {
			return "", false
		}
	}

	identifiers := []string{}
	seen := map[string]bool{}
	addIdentifier := func(value *string) {
		if value == nil {
			return
		}
		normalized := strings.TrimSpace(*value)
		key := keeperAuthAliasKey(normalized)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		identifiers = append(identifiers, normalized)
	}
	addIdentifier(record.AuthIndex)
	addIdentifier(record.Source)
	for _, field := range []string{"auth_index", "authIndex", "index", "auth_name", "authName", "account_id", "accountId", "email", "account_email", "accountEmail", "user_email", "userEmail"} {
		addIdentifier(rawJSONStringField(record.RawJSON, field))
	}
	for _, identifier := range identifiers {
		if name, ok := keeperSingleAuthNameForUsageIdentifier(identifier, aliases); ok {
			return name, true
		}
	}
	return "", false
}

func keeperUsageRecordSourceAccount(record UsageRecord) string {
	if record.SourceAccount != nil {
		return strings.ToLower(strings.TrimSpace(*record.SourceAccount))
	}
	if sourceAccount := sourceAccountFromUsageSource(record.Source); sourceAccount != nil {
		return *sourceAccount
	}
	return ""
}

func keeperSingleAuthNameForUsageIdentifier(identifier string, aliases map[string][]string) (string, bool) {
	names := keeperAuthNamesForUsageIdentifier(identifier, aliases)
	if len(names) != 1 {
		return "", false
	}
	normalized := strings.TrimSpace(names[0])
	if normalized == "" {
		return "", false
	}
	return normalized, true
}

func keeperRecordInQuotaWindow(record UsageRecord, usage *keeperQuotaWindowUsage) bool {
	if usage == nil {
		return false
	}
	return !record.Timestamp.Before(usage.WindowStart) && record.Timestamp.Before(usage.WindowEnd)
}

func addRecordToKeeperQuotaWindowUsage(usage *keeperQuotaWindowUsage, record UsageRecord, prices map[[2]string]ModelPrice) {
	if usage == nil {
		return
	}
	usage.Records++
	if record.Failed {
		usage.FailedRecords++
	} else {
		usage.SuccessRecords++
	}
	usage.InputTokens += usageAggregateInputTokens(record)
	usage.OutputTokens += record.OutputTokens
	usage.CachedTokens += record.CachedTokens
	usage.ReasoningTokens += record.ReasoningTokens
	usage.TotalTokens += usageAggregateTotalTokens(record)
	amount, unpriced := recordCost(record, prices)
	if unpriced {
		usage.UnpricedRecords++
		return
	}
	usage.EstimatedCostUSD = mathRound(usage.EstimatedCostUSD+amount, 8)
}

func (a *App) updateKeeperSettings(w http.ResponseWriter, r *http.Request) error {
	var payload keeperSettingsUpdateRequest
	if err := decodeJSON(r, &payload); err != nil {
		return err
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	if payload.ScheduleCron != nil {
		_, normalized, err := nextRunTimes(*payload.ScheduleCron, 5, time.Now())
		if err != nil {
			return err
		}
		cfg.CodexKeeper.ScheduleCron = normalized
	}
	if payload.QuotaThreshold != nil {
		if *payload.QuotaThreshold < 0 || *payload.QuotaThreshold > 100 {
			return validationError("quota_threshold 超出范围")
		}
		cfg.CodexKeeper.QuotaThreshold = *payload.QuotaThreshold
	}
	if payload.UsageTimeoutSeconds != nil {
		if *payload.UsageTimeoutSeconds < 1 {
			return validationError("usage_timeout_seconds 不能小于 1")
		}
		cfg.CodexKeeper.UsageTimeoutSeconds = *payload.UsageTimeoutSeconds
	}
	if payload.CPATimeoutSeconds != nil {
		if *payload.CPATimeoutSeconds < 1 {
			return validationError("cpa_timeout_seconds 不能小于 1")
		}
		cfg.CodexKeeper.CPATimeoutSeconds = *payload.CPATimeoutSeconds
	}
	if payload.MaxRetries != nil {
		if *payload.MaxRetries < 0 || *payload.MaxRetries > 5 {
			return validationError("max_retries 超出范围")
		}
		cfg.CodexKeeper.MaxRetries = *payload.MaxRetries
	}
	if payload.WorkerThreads != nil {
		if *payload.WorkerThreads < 1 || *payload.WorkerThreads > 64 {
			return validationError("worker_threads 超出范围")
		}
		cfg.CodexKeeper.WorkerThreads = *payload.WorkerThreads
	}
	if payload.ConditionalRefreshIntervalSeconds != nil {
		if !validKeeperConditionalRefreshInterval(*payload.ConditionalRefreshIntervalSeconds) {
			return validationError("conditional_refresh_interval_seconds 超出范围")
		}
		cfg.CodexKeeper.ConditionalRefreshIntervalSeconds = *payload.ConditionalRefreshIntervalSeconds
	}
	if payload.AccountRefreshCacheMinutes != nil {
		if *payload.AccountRefreshCacheMinutes < 1 {
			return validationError("account_refresh_cache_minutes 不能小于 1")
		}
		cfg.CodexKeeper.AccountRefreshCacheMinutes = *payload.AccountRefreshCacheMinutes
	}
	if payload.DryRun != nil {
		cfg.CodexKeeper.DryRun = *payload.DryRun
	}
	if payload.EnableCredentialWebsockets != nil {
		cfg.CodexKeeper.EnableCredentialWebsockets = *payload.EnableCredentialWebsockets
	}
	if payload.AutoStartDaemon != nil {
		cfg.CodexKeeper.AutoStartDaemon = *payload.AutoStartDaemon
	}
	if payload.PriorityRules != nil {
		rules := map[string]int{}
		for _, item := range payload.PriorityRules {
			key := strings.ToLower(strings.TrimSpace(item.AccountType))
			if key == "" {
				return validationError("凭证类型不能为空")
			}
			if item.Priority < 0 || item.Priority > 20 {
				return validationError("priority 超出范围")
			}
			rules[key] = item.Priority
		}
		cfg.CodexKeeperPriorityRule = normalizePriorityRules(rules)
	}
	if err := a.saveConfig(r.Context(), cfg); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, keeperSettingsResponse(cfg))
	return nil
}

func (a *App) executeKeeperRun(ctx context.Context, mode string, logFn func(string)) (keeperStats, string, error) {
	return a.executeKeeperRunForAccounts(ctx, mode, nil, logFn)
}

type keeperRunOptions struct {
	Mode            string
	AuthNames       []string
	ManualRefresh   bool
	UseRefreshCache bool
	PersistRun      bool
	TryLockAuthName func(string, string) bool
	UnlockAuthName  func(string)
}

func (a *App) executeKeeperRunForAccounts(ctx context.Context, mode string, authNames []string, logFn func(string)) (keeperStats, string, error) {
	return a.executeKeeperRunWithOptions(ctx, keeperRunOptionsForMode(mode, authNames), logFn)
}

func keeperRunOptionsForMode(mode string, authNames []string) keeperRunOptions {
	return keeperRunOptions{
		Mode:            mode,
		AuthNames:       authNames,
		ManualRefresh:   mode == "accounts",
		UseRefreshCache: mode == "daemon" || mode == "conditional",
		PersistRun:      keeperModePersistsRun(mode),
	}
}

func keeperModePersistsRun(mode string) bool {
	return mode != "accounts" && mode != "conditional"
}

func (a *App) executeKeeperRunWithOptions(ctx context.Context, options keeperRunOptions, logFn func(string)) (keeperStats, string, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return keeperStats{}, "", err
	}
	if strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return keeperStats{}, "", validationError("管理密钥未设置，无法运行 Codex Keeper")
	}
	runID := 0
	if options.PersistRun {
		runID, err = a.createKeeperRun(ctx, options.Mode)
		if err != nil {
			return keeperStats{}, "", err
		}
	}
	targetNames, err := normalizeOptionalKeeperAuthNames(options.AuthNames)
	if err != nil {
		if runID > 0 {
			_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), keeperStats{})
		}
		return keeperStats{}, "", err
	}
	targetSet := map[string]bool{}
	for _, name := range targetNames {
		targetSet[name] = true
	}
	if options.Mode == "conditional" {
		logFn(fmt.Sprintf("开始按条件刷新 %d 份 Codex 凭证", len(targetSet)))
	} else if len(targetSet) > 0 {
		logFn(fmt.Sprintf("开始刷新 %d 份 Codex 凭证", len(targetSet)))
	} else {
		logFn("开始 Codex 凭证巡检")
	}
	stats := keeperStats{}
	detail := "巡检完成"
	authFiles, err := a.listKeeperRemoteAuthFiles(ctx, cfg)
	if err != nil {
		if runID > 0 {
			_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
		}
		return stats, "", err
	}
	filtered := make([]map[string]any, 0, len(authFiles))
	remoteCodexNames := map[string]bool{}
	for _, item := range authFiles {
		if !keeperIsCodexProvider(keeperCredentialProvider(item)) {
			continue
		}
		name := keeperString(item["name"])
		if name != "" {
			remoteCodexNames[name] = true
		}
		if len(targetSet) == 0 || targetSet[name] {
			filtered = append(filtered, item)
		}
	}
	if len(targetSet) == 0 {
		pruned, err := a.pruneKeeperMissingAuthStates(ctx, remoteCodexNames)
		if err != nil {
			if runID > 0 {
				_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
			}
			return stats, "", err
		}
		if pruned > 0 {
			logFn(fmt.Sprintf("清理本地已不存在的 Codex 凭证 %d 份", pruned))
		}
	}
	stats.Total = len(filtered)
	if cfg.CodexKeeper.EnableCredentialWebsockets && !cfg.CodexKeeper.DryRun {
		var websocketFailures []keeperAccountResult
		filtered, websocketFailures = a.ensureKeeperAuthWebsockets(ctx, cfg, options.Mode, filtered, logFn, options.TryLockAuthName, options.UnlockAuthName)
		for _, result := range websocketFailures {
			a.mergeKeeperStats(&stats, result)
			if runID > 0 {
				if err := a.recordKeeperRunAccount(ctx, runID, result); err != nil {
					logFn("写入巡检凭证历史失败：" + err.Error())
				}
			}
		}
	}
	if options.UseRefreshCache {
		var skippedNames []string
		filtered, skippedNames, err = a.filterKeeperCachedAuthItems(ctx, filtered, cfg)
		if err != nil {
			if runID > 0 {
				_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
			}
			return stats, "", err
		}
		stats.Skipped += len(skippedNames)
		if err := a.addKeeperCachedAuthStats(ctx, &stats, skippedNames); err != nil {
			if runID > 0 {
				_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
			}
			return stats, "", err
		}
	}
	if len(filtered) == 0 {
		if stats.NetworkError > 0 {
			detail = fmt.Sprintf("巡检完成：网络错误 %d", stats.NetworkError)
		} else if stats.Total > 0 && options.UseRefreshCache {
			detail = "缓存时间内没有需要自动刷新的 Codex 认证文件"
		} else if len(targetSet) > 0 {
			detail = "未发现指定 Codex 认证文件"
		} else {
			detail = "未发现 Codex 认证文件"
		}
		if runID > 0 {
			_ = a.finishKeeperRun(ctx, runID, "completed", detail, stats)
		}
		return stats, detail, nil
	}
	for _, item := range filtered {
		name := keeperString(item["name"])
		locked := false
		if options.TryLockAuthName != nil && name != "" {
			if !options.TryLockAuthName(options.Mode, name) {
				stats.Skipped++
				logFn(name + ": 正在其他 Keeper 任务处理中，跳过")
				continue
			}
			locked = true
		}
		unlock := func() {
			if locked && options.UnlockAuthName != nil {
				options.UnlockAuthName(name)
				locked = false
			}
		}
		if locked && options.UseRefreshCache {
			cutoff := time.Now().In(appTimeLocation).Add(-keeperRefreshCacheDuration(cfg))
			cached, err := a.keeperAuthCheckedSince(ctx, name, cutoff)
			if err != nil {
				unlock()
				if runID > 0 {
					_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
				}
				return stats, "", err
			}
			if cached {
				unlock()
				stats.Skipped++
				if err := a.addKeeperCachedAuthStats(ctx, &stats, []string{name}); err != nil {
					if runID > 0 {
						_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
					}
					return stats, "", err
				}
				logFn(name + ": 缓存时间内已刷新，跳过")
				continue
			}
		}
		result := a.processKeeperAuth(ctx, cfg, item, logFn, options.ManualRefresh)
		unlock()
		a.mergeKeeperStats(&stats, result)
		if runID > 0 {
			if err := a.recordKeeperRunAccount(ctx, runID, result); err != nil {
				logFn("写入巡检凭证历史失败：" + err.Error())
			}
		}
	}
	if options.Mode == "conditional" {
		detail = fmt.Sprintf("条件刷新完成：健康 %d，坏凭证禁用 %d，恢复启用 %d，优先级降级 %d，优先级恢复 %d，网络错误 %d，缓存跳过 %d", stats.Healthy, stats.StatusDisabled, stats.StatusEnabled, stats.PriorityDegraded, stats.PriorityRestored, stats.NetworkError, stats.Skipped)
	} else if len(targetSet) > 0 {
		detail = fmt.Sprintf("凭证刷新完成：健康 %d，凭证异常 %d，恢复启用 %d，优先级降级 %d，优先级恢复 %d，网络错误 %d", stats.Healthy, stats.StatusDisabled, stats.StatusEnabled, stats.PriorityDegraded, stats.PriorityRestored, stats.NetworkError)
	} else {
		detail = fmt.Sprintf("巡检完成：健康 %d，坏凭证禁用 %d，恢复启用 %d，优先级降级 %d，网络错误 %d，缓存跳过 %d", stats.Healthy, stats.StatusDisabled, stats.StatusEnabled, stats.PriorityDegraded, stats.NetworkError, stats.Skipped)
	}
	if runID > 0 {
		_ = a.finishKeeperRun(ctx, runID, "completed", detail, stats)
	}
	return stats, detail, nil
}

func keeperRefreshCacheDuration(cfg AppConfig) time.Duration {
	minutes := cfg.CodexKeeper.AccountRefreshCacheMinutes
	if minutes < 1 {
		minutes = 10
	}
	return time.Duration(minutes) * time.Minute
}

func (a *App) conditionalKeeperRefreshCandidates(ctx context.Context, cfg AppConfig) ([]string, error) {
	cacheWindow := keeperRefreshCacheDuration(cfg)
	since := time.Now().In(appTimeLocation).Add(-cacheWindow)
	aliases, err := a.keeperAuthNameAliases(ctx)
	if err != nil {
		return nil, err
	}
	names := []string{}
	seen := map[string]bool{}
	usageIdentifiers := []string{}
	seenUsageIdentifiers := map[string]bool{}
	addName := func(name string) {
		normalized := strings.TrimSpace(name)
		if normalized == "" || seen[normalized] {
			return
		}
		seen[normalized] = true
		names = append(names, normalized)
	}
	addUsageIdentifier := func(identifier string, allowOpaque bool) bool {
		normalized := strings.TrimSpace(identifier)
		aliasKey := keeperAuthAliasKey(normalized)
		if aliasKey == "" || seenUsageIdentifiers[aliasKey] {
			return len(keeperAuthNamesForUsageIdentifier(normalized, aliases)) > 0
		}
		if !allowOpaque && !keeperLooksLikeAuthIdentifier(normalized, aliases) {
			return false
		}
		seenUsageIdentifiers[aliasKey] = true
		usageIdentifiers = append(usageIdentifiers, normalized)
		resolved := false
		for _, name := range keeperAuthNamesForUsageIdentifier(normalized, aliases) {
			addName(name)
			resolved = true
		}
		return resolved
	}

	rows, err := a.db.QueryContext(ctx, `
		SELECT source, raw_json
		FROM usage_records
		WHERE timestamp >= ?
		ORDER BY timestamp DESC
	`, dbTime(since))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var source sql.NullString
		var rawJSON string
		if err := rows.Scan(&source, &rawJSON); err != nil {
			_ = rows.Close()
			return nil, err
		}
		sourceResolved := false
		if source.Valid {
			sourceResolved = addUsageIdentifier(source.String, false)
		}
		if identifier := rawJSONStringField(rawJSON, "source"); identifier != nil {
			sourceResolved = addUsageIdentifier(*identifier, false) || sourceResolved
		}
		if sourceResolved {
			continue
		}
		for _, field := range []string{"auth_index", "authIndex", "index", "auth_name", "authName", "account_id", "accountId"} {
			if identifier := rawJSONStringField(rawJSON, field); identifier != nil {
				addUsageIdentifier(*identifier, true)
			}
		}
		for _, field := range []string{"email", "account_email", "accountEmail", "user_email", "userEmail"} {
			if identifier := rawJSONStringField(rawJSON, field); identifier != nil {
				addUsageIdentifier(*identifier, false)
			}
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, name := range a.keeperAuthNamesFromRemoteUsageIdentifiers(ctx, cfg, usageIdentifiers, aliases) {
		addName(name)
	}
	if err := a.reconcileKeeperConditionalRemoteAuthStates(ctx, cfg, addName); err != nil {
		return nil, err
	}

	rows, err = a.db.QueryContext(ctx, `
		SELECT auth_name
		FROM codex_keeper_auth_states
		WHERE disabled = 0
		  AND (
		      (primary_reset_at IS NOT NULL AND primary_reset_at <= ?)
		   OR (secondary_reset_at IS NOT NULL AND secondary_reset_at <= ?)
		  )
		ORDER BY auth_name
	`, dbTime(time.Now().In(appTimeLocation)), dbTime(time.Now().In(appTimeLocation)))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return nil, err
		}
		addName(name)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = a.db.QueryContext(ctx, `
		SELECT auth_name
		FROM codex_keeper_auth_states
		WHERE disabled = 0
		  AND last_error IS NOT NULL
		  AND TRIM(last_error) <> ''
		ORDER BY auth_name
	`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return nil, err
		}
		addName(name)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	enabledNames, err := a.filterKeeperEnabledAuthNames(ctx, names)
	if err != nil {
		return nil, err
	}
	filtered, _, err := a.filterKeeperCachedAuthNames(ctx, enabledNames, cfg)
	return filtered, err
}

func (a *App) reconcileKeeperConditionalRemoteAuthStates(ctx context.Context, cfg AppConfig, addName func(string)) error {
	if strings.TrimSpace(cfg.Collector.CLIProxyURL) == "" {
		return nil
	}
	authFiles, err := a.listKeeperRemoteAuthFiles(ctx, cfg)
	if err != nil {
		return err
	}
	remoteNames := map[string]bool{}
	refreshableRemoteNames := map[string]bool{}
	for _, item := range authFiles {
		if !keeperIsCodexProvider(keeperCredentialProvider(item)) {
			continue
		}
		name := keeperString(item["name"])
		if name == "" {
			continue
		}
		remoteNames[name] = true
		if !keeperBool(item["disabled"]) {
			refreshableRemoteNames[name] = true
		}
	}
	localNames, err := a.keeperAuthStateNameSet(ctx)
	if err != nil {
		return err
	}
	for name := range refreshableRemoteNames {
		if !localNames[name] && !a.keeperRemoteAuthDisabledForConditional(ctx, cfg, name, authFiles) {
			addName(name)
		}
	}
	_, err = a.pruneKeeperMissingAuthStates(ctx, remoteNames)
	return err
}

func (a *App) keeperRemoteAuthDisabledForConditional(ctx context.Context, cfg AppConfig, name string, authFiles []map[string]any) bool {
	for _, item := range authFiles {
		if keeperString(item["name"]) != name {
			continue
		}
		if keeperBool(item["disabled"]) {
			return true
		}
		if _, ok := item["disabled"]; ok {
			return false
		}
		detail, err := a.getKeeperRemoteAuthFile(ctx, cfg, name)
		if err != nil || detail == nil {
			return false
		}
		return keeperBool(mergeKeeperObjects(item, detail)["disabled"])
	}
	return false
}

func (a *App) keeperAuthStateNameSet(ctx context.Context) (map[string]bool, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT auth_name FROM codex_keeper_auth_states`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

func (a *App) keeperAuthNameAliases(ctx context.Context) (map[string][]string, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT auth_name, email
		FROM codex_keeper_auth_states
		WHERE disabled = 0
		ORDER BY auth_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	aliases := map[string][]string{}
	for rows.Next() {
		var name string
		var email sql.NullString
		if err := rows.Scan(&name, &email); err != nil {
			return nil, err
		}
		addKeeperAuthAlias(aliases, name, name)
		if email.Valid {
			addKeeperAuthAlias(aliases, email.String, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return aliases, nil
}

func (a *App) keeperAuthNamesFromRemoteUsageIdentifiers(ctx context.Context, cfg AppConfig, identifiers []string, aliases map[string][]string) []string {
	if !keeperHasUnresolvedUsageIdentifiers(identifiers, aliases) || strings.TrimSpace(cfg.Collector.CLIProxyURL) == "" {
		return nil
	}
	authFiles, err := a.listKeeperRemoteAuthFiles(ctx, cfg)
	if err != nil {
		return nil
	}
	codexFiles := make([]map[string]any, 0, len(authFiles))
	for _, item := range authFiles {
		if !keeperIsCodexProvider(keeperCredentialProvider(item)) {
			continue
		}
		if keeperBool(item["disabled"]) {
			continue
		}
		codexFiles = append(codexFiles, item)
		addKeeperAuthObjectAliases(aliases, item)
	}
	if !keeperHasUnresolvedUsageIdentifiers(identifiers, aliases) {
		return keeperAuthNamesForUsageIdentifiers(identifiers, aliases)
	}
	for _, item := range codexFiles {
		name := keeperString(item["name"])
		if name == "" {
			continue
		}
		detail, err := a.getKeeperRemoteAuthFile(ctx, cfg, name)
		if err != nil || detail == nil {
			continue
		}
		merged := mergeKeeperObjects(item, detail)
		if keeperBool(merged["disabled"]) {
			continue
		}
		addKeeperAuthObjectAliases(aliases, merged)
		if !keeperHasUnresolvedUsageIdentifiers(identifiers, aliases) {
			break
		}
	}
	return keeperAuthNamesForUsageIdentifiers(identifiers, aliases)
}

func keeperHasUnresolvedUsageIdentifiers(identifiers []string, aliases map[string][]string) bool {
	for _, identifier := range identifiers {
		if len(keeperAuthNamesForUsageIdentifier(identifier, aliases)) == 0 {
			return true
		}
	}
	return false
}

func keeperAuthNamesForUsageIdentifiers(identifiers []string, aliases map[string][]string) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, identifier := range identifiers {
		for _, name := range keeperAuthNamesForUsageIdentifier(identifier, aliases) {
			normalized := strings.TrimSpace(name)
			if normalized == "" || seen[normalized] {
				continue
			}
			seen[normalized] = true
			names = append(names, normalized)
		}
	}
	return names
}

func keeperAuthNamesForUsageIdentifier(identifier string, aliases map[string][]string) []string {
	normalized := strings.TrimSpace(identifier)
	if normalized == "" {
		return nil
	}
	if names := aliases[keeperAuthAliasKey(normalized)]; len(names) > 0 {
		return names
	}
	if strings.HasSuffix(strings.ToLower(normalized), ".json") {
		return []string{normalized}
	}
	return nil
}

func keeperLooksLikeAuthIdentifier(identifier string, aliases map[string][]string) bool {
	normalized := strings.TrimSpace(identifier)
	if normalized == "" {
		return false
	}
	if len(aliases[keeperAuthAliasKey(normalized)]) > 0 {
		return true
	}
	lower := strings.ToLower(normalized)
	return strings.HasSuffix(lower, ".json") || strings.Contains(normalized, "@")
}

func addKeeperAuthObjectAliases(aliases map[string][]string, object map[string]any) {
	name := keeperString(object["name"])
	if name == "" {
		return
	}
	addKeeperAuthAlias(aliases, name, name)
	for _, key := range []string{"auth_name", "authName", "auth_index", "authIndex", "index", "source", "email", "account_email", "accountEmail", "user_email", "userEmail", "account_id", "accountId"} {
		if value := keeperAliasString(object[key]); value != "" {
			addKeeperAuthAlias(aliases, value, name)
		}
	}
}

func addKeeperAuthAlias(aliases map[string][]string, alias string, name string) {
	normalizedAlias := keeperAuthAliasKey(alias)
	normalizedName := strings.TrimSpace(name)
	if normalizedAlias == "" || normalizedName == "" {
		return
	}
	for _, existing := range aliases[normalizedAlias] {
		if existing == normalizedName {
			return
		}
	}
	aliases[normalizedAlias] = append(aliases[normalizedAlias], normalizedName)
}

func keeperAliasString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(typed, 'f', -1, 64))
	case int:
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

func keeperAuthAliasKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (a *App) filterKeeperCachedAuthItems(ctx context.Context, items []map[string]any, cfg AppConfig) ([]map[string]any, []string, error) {
	if len(items) == 0 {
		return items, nil, nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		if name := keeperString(item["name"]); name != "" {
			names = append(names, name)
		}
	}
	allowedNames, skippedNames, err := a.filterKeeperCachedAuthNames(ctx, names, cfg)
	if err != nil {
		return nil, nil, err
	}
	allowed := map[string]bool{}
	for _, name := range allowedNames {
		allowed[name] = true
	}
	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		name := keeperString(item["name"])
		if name == "" || allowed[name] {
			filtered = append(filtered, item)
		}
	}
	return filtered, skippedNames, nil
}

func (a *App) filterKeeperCachedAuthNames(ctx context.Context, names []string, cfg AppConfig) ([]string, []string, error) {
	normalized, err := normalizeOptionalKeeperAuthNames(names)
	if err != nil {
		return nil, nil, err
	}
	if len(normalized) == 0 {
		return normalized, nil, nil
	}
	cutoff := time.Now().In(appTimeLocation).Add(-keeperRefreshCacheDuration(cfg))
	filtered := make([]string, 0, len(normalized))
	skipped := []string{}
	for _, name := range normalized {
		cached, err := a.keeperAuthCheckedSince(ctx, name, cutoff)
		if err != nil {
			return nil, nil, err
		}
		if cached {
			skipped = append(skipped, name)
			continue
		}
		filtered = append(filtered, name)
	}
	return filtered, skipped, nil
}

func (a *App) filterKeeperEnabledAuthNames(ctx context.Context, names []string) ([]string, error) {
	normalized, err := normalizeOptionalKeeperAuthNames(names)
	if err != nil {
		return nil, err
	}
	if len(normalized) == 0 {
		return normalized, nil
	}
	rows, err := a.db.QueryContext(ctx, `
		SELECT auth_name
		FROM codex_keeper_auth_states
		WHERE disabled = 1
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	disabledNames := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		disabledNames[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(normalized))
	for _, name := range normalized {
		if disabledNames[name] {
			continue
		}
		filtered = append(filtered, name)
	}
	return filtered, nil
}

func (a *App) addKeeperCachedAuthStats(ctx context.Context, stats *keeperStats, names []string) error {
	cachedStats, err := a.keeperCachedAuthStats(ctx, names)
	if err != nil {
		return err
	}
	stats.add(cachedStats)
	return nil
}

func (a *App) keeperCachedAuthStats(ctx context.Context, names []string) (keeperStats, error) {
	normalized, err := normalizeOptionalKeeperAuthNames(names)
	if err != nil {
		return keeperStats{}, err
	}
	stats := keeperStats{}
	for _, name := range normalized {
		state, err := a.getKeeperState(ctx, name)
		if err != nil {
			var appErr *AppError
			if errors.As(err, &appErr) && appErr.Code == "not_found" {
				continue
			}
			return keeperStats{}, err
		}
		stats.mergeCachedState(*state)
	}
	return stats, nil
}

func (a *App) keeperAuthCheckedSince(ctx context.Context, name string, cutoff time.Time) (bool, error) {
	var lastChecked sql.NullString
	err := a.db.QueryRowContext(ctx, `
		SELECT CAST(last_checked_at AS TEXT)
		FROM codex_keeper_auth_states
		WHERE auth_name = ?
	`, name).Scan(&lastChecked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !lastChecked.Valid {
		return false, nil
	}
	checkedAt, ok := parseDBTime(lastChecked.String)
	if !ok {
		return false, nil
	}
	return checkedAt.After(cutoff) || checkedAt.Equal(cutoff), nil
}

func (a *App) ensureKeeperAuthWebsockets(
	ctx context.Context,
	cfg AppConfig,
	mode string,
	items []map[string]any,
	logFn func(string),
	tryLock func(string, string) bool,
	unlock func(string),
) ([]map[string]any, []keeperAccountResult) {
	if len(items) == 0 {
		return items, nil
	}
	remaining := make([]map[string]any, 0, len(items))
	failures := []keeperAccountResult{}
	now := time.Now().In(appTimeLocation)
	for _, item := range items {
		name := keeperString(item["name"])
		if name == "" || keeperBool(item["websockets"]) {
			remaining = append(remaining, item)
			continue
		}
		locked := false
		if tryLock != nil && unlock != nil {
			if !tryLock(mode, name) {
				remaining = append(remaining, item)
				continue
			}
			locked = true
		}
		unlockIfNeeded := func() {
			if locked && unlock != nil {
				unlock(name)
				locked = false
			}
		}
		if err := a.setKeeperRemoteWebsockets(ctx, cfg, name); err != nil {
			message := "启用 WebSocket 传输失败：" + err.Error()
			disabled := keeperBool(item["disabled"])
			result := keeperAccountResult{
				Name:         name,
				AuthIndex:    keeperRemoteAuthIndex(item),
				AccountType:  accountTypeFromKeeperDetail(item, nil),
				Disabled:     &disabled,
				Priority:     keeperIntPtr(item["priority"]),
				Result:       "network_error",
				LastError:    &message,
				LatestAction: &message,
				CheckedAt:    now,
			}
			_ = a.upsertKeeperState(ctx, result)
			logFn(name + ": " + message)
			failures = append(failures, result)
			unlockIfNeeded()
			continue
		}
		item["websockets"] = true
		unlockIfNeeded()
		logFn(name + ": 已启用 WebSocket 传输")
		remaining = append(remaining, item)
	}
	return remaining, failures
}

func (a *App) processKeeperAuth(ctx context.Context, cfg AppConfig, authInfo map[string]any, logFn func(string), manualRefresh bool) keeperAccountResult {
	now := time.Now().In(appTimeLocation)
	name := keeperString(authInfo["name"])
	if name == "" {
		name = "unknown"
	}
	result := keeperAccountResult{Name: name, Result: "skipped", CheckedAt: now}
	detail, err := a.getKeeperRemoteAuthFile(ctx, cfg, name)
	if err != nil {
		message := "读取认证文件详情失败：" + err.Error()
		result.Result = "network_error"
		result.LastError = &message
		result.LatestAction = &message
		_ = a.upsertKeeperState(ctx, result)
		logFn(name + ": " + message)
		return result
	}
	if detail == nil {
		message := "读取认证文件详情失败"
		result.Result = "network_error"
		result.LastError = &message
		result.LatestAction = &message
		_ = a.upsertKeeperState(ctx, result)
		return result
	}
	merged := mergeKeeperObjects(authInfo, detail)
	result.Email = keeperStringPtr(merged["email"], merged["account_email"], merged["user_email"])
	result.AuthIndex = keeperRemoteAuthIndex(merged)
	result.Priority = keeperIntPtr(merged["priority"])
	disabled := keeperBool(merged["disabled"])
	result.Disabled = &disabled
	result.AccountType = accountTypeFromKeeperDetail(merged, nil)
	var state *keeperAuthState
	var restorePriority *int
	if loadedState, err := a.getKeeperState(ctx, name); err == nil {
		state = loadedState
		restorePriority = loadedState.RestorePriority
	}
	recoverableUnauthorizedDisabled := disabled && isKeeperRecoverableUnauthorizedDisabledState(state)
	if disabled && !manualRefresh && !recoverableUnauthorizedDisabled {
		result.Result = "disabled"
		a.preserveKeeperBadCredentialDiagnosis(ctx, &result)
		_ = a.upsertKeeperState(ctx, result)
		return result
	}
	if keeperString(merged["access_token"]) == "" {
		message := "缺少 access token"
		action := "刷新发现凭证不可用：" + message
		if !cfg.CodexKeeper.DryRun {
			if !disabled {
				if err := a.setKeeperRemoteDisabled(ctx, cfg, name, true); err != nil {
					message = "禁用坏凭证失败：" + err.Error()
					result.LastError = &message
					result.Result = "network_error"
					_ = a.upsertKeeperState(ctx, result)
					return result
				}
			}
			_ = a.setKeeperRemotePriority(ctx, cfg, name, nil)
			disabled = true
			result.Disabled = &disabled
			result.Priority = nil
			action = "禁用凭证：" + message
		} else {
			action = "模拟禁用：" + message
		}
		result.Result = "status_disabled"
		result.LastError = &message
		result.LatestAction = &action
		_ = a.upsertKeeperState(ctx, result)
		logFn(name + ": " + action)
		return result
	}

	usageResult := a.checkKeeperUsage(ctx, cfg, merged)
	if usageResult.StatusCode == nil {
		message := "网络检测失败：" + usageResult.Error
		result.Result = "network_error"
		result.LastError = &message
		result.LatestAction = &message
		_ = a.upsertKeeperState(ctx, result)
		logFn(name + ": " + message)
		return result
	}
	result.LastStatusCode = usageResult.StatusCode
	if isBadKeeperCredential(usageResult) {
		message := fmt.Sprintf("凭证不可用：HTTP %d", *usageResult.StatusCode)
		if usageResult.Brief != "" {
			message += "，" + usageResult.Brief
		}
		action := "刷新发现凭证不可用：" + message
		if !cfg.CodexKeeper.DryRun {
			if !disabled {
				if err := a.setKeeperRemoteDisabled(ctx, cfg, name, true); err != nil {
					message = "禁用坏凭证失败：" + err.Error()
					result.Result = "network_error"
					result.LastError = &message
					_ = a.upsertKeeperState(ctx, result)
					return result
				}
			}
			_ = a.setKeeperRemotePriority(ctx, cfg, name, nil)
			disabled = true
			result.Disabled = &disabled
			result.Priority = nil
			action = "禁用凭证：" + message
		} else {
			action = "模拟禁用：" + message
		}
		result.Result = "status_disabled"
		result.LastError = &message
		result.LatestAction = &action
		_ = a.upsertKeeperState(ctx, result)
		logFn(name + ": " + action)
		return result
	}
	if *usageResult.StatusCode < 200 || *usageResult.StatusCode >= 300 {
		message := fmt.Sprintf("usage 检测失败：HTTP %d", *usageResult.StatusCode)
		if usageResult.Brief != "" {
			message += "，" + usageResult.Brief
		}
		result.Result = "network_error"
		result.LastError = &message
		result.LatestAction = &message
		_ = a.upsertKeeperState(ctx, result)
		return result
	}
	if resetCredits := keeperEmbeddedResetCredits(usageResult.JSONData); resetCredits != nil {
		a.storeKeeperResetCredits(name, keeperResetCreditsResponseFromBody(resetCredits, time.Now()))
	}
	usage := parseKeeperUsageInfo(usageResult.JSONData)
	result.AccountType = accountTypeFromKeeperDetail(merged, &usage)
	result.PrimaryUsedPercent = &usage.PrimaryUsedPercent
	result.SecondaryUsedPercent = usage.SecondaryUsedPercent
	result.PrimaryResetAt = usage.PrimaryResetAt
	result.SecondaryResetAt = usage.SecondaryResetAt
	result.PrimaryWindowSeconds = usage.PrimaryWindowSeconds
	result.SecondaryWindowSeconds = usage.SecondaryWindowSeconds
	result.QuotaThreshold = &cfg.CodexKeeper.QuotaThreshold
	result.Result = "healthy"

	if recoverableUnauthorizedDisabled {
		action := fmt.Sprintf("恢复启用：usage 检测恢复 HTTP %d", *usageResult.StatusCode)
		if !cfg.CodexKeeper.DryRun {
			if err := a.setKeeperRemoteDisabled(ctx, cfg, name, false); err != nil {
				message := "恢复启用失败：" + err.Error()
				result.Result = "network_error"
				result.LastError = &message
				result.LatestAction = &message
				_ = a.upsertKeeperState(ctx, result)
				logFn(name + ": " + message)
				return result
			}
			disabled = false
			result.Disabled = &disabled
		} else {
			action = "模拟" + action
		}
		result.Result = "status_enabled"
		result.LatestAction = &action
		result.ClearRestorePriority = true
		result.LastError = nil
		_ = a.upsertKeeperState(ctx, result)
		logFn(name + ": " + action)
		return result
	}

	action := a.applyKeeperPriorityPolicy(ctx, cfg, name, result.AccountType, result.Priority, restorePriority, usage)
	if action != nil {
		result.LatestAction = &action.Message
		if action.Result == "priority_degraded" {
			result.Result = "priority_degraded"
			result.Priority = action.Priority
			result.RestorePriority = action.RestorePriority
		}
		if action.Result == "priority_restored" {
			result.Result = "priority_restored"
			result.Priority = action.Priority
			result.ClearRestorePriority = true
		}
		logFn(name + ": " + action.Message)
	} else {
		accountType := "unknown"
		if result.AccountType != nil && strings.TrimSpace(*result.AccountType) != "" {
			accountType = *result.AccountType
		}
		if manualRefresh {
			action := fmt.Sprintf("刷新完成，类型 %s", accountType)
			result.LatestAction = &action
			logFn(name + ": " + action)
		} else {
			logFn(fmt.Sprintf("%s: 巡检正常，类型 %s", name, accountType))
		}
	}
	if result.Priority == nil || *result.Priority != -1 {
		result.ClearRestorePriority = true
	}
	result.LastError = nil
	_ = a.upsertKeeperState(ctx, result)
	return result
}

type keeperPriorityPolicyAction struct {
	Message         string
	Result          string
	Priority        *int
	RestorePriority *int
}

func (a *App) applyKeeperPriorityPolicy(ctx context.Context, cfg AppConfig, name string, accountType *string, priority *int, restorePriority *int, usage keeperUsageInfo) *keeperPriorityPolicyAction {
	quotaReached := usage.PrimaryUsedPercent >= cfg.CodexKeeper.QuotaThreshold ||
		(usage.SecondaryUsedPercent != nil && *usage.SecondaryUsedPercent >= cfg.CodexKeeper.QuotaThreshold)
	currentPriority := keeperEffectivePriority(priority)
	next := keeperPriorityForType(accountType, cfg.CodexKeeperPriorityRule)
	if quotaReached {
		if currentPriority <= -1 {
			return nil
		}
		restoreTo := restorePriority
		if restoreTo == nil {
			restoreTo = next
		}
		if currentPriority > 20 {
			restoreTo = &currentPriority
		}
		if restoreTo == nil {
			restoreTo = &currentPriority
		}
		message := fmt.Sprintf("降为低优先级：额度使用率达到阈值 %d%%", cfg.CodexKeeper.QuotaThreshold)
		if cfg.CodexKeeper.DryRun {
			message = "模拟" + message
			low := -1
			return &keeperPriorityPolicyAction{Message: message, Result: "priority_degraded", Priority: &low, RestorePriority: restoreTo}
		}
		low := -1
		if err := a.setKeeperRemotePriority(ctx, cfg, name, &low); err != nil {
			message = "写入低优先级失败：" + err.Error()
			return &keeperPriorityPolicyAction{Message: message}
		}
		return &keeperPriorityPolicyAction{Message: message, Result: "priority_degraded", Priority: &low, RestorePriority: restoreTo}
	}
	if currentPriority == -1 {
		restoreTo := restorePriority
		if restoreTo == nil {
			restoreTo = next
		}
		if restoreTo == nil {
			zero := 0
			restoreTo = &zero
		}
		message := fmt.Sprintf("恢复优先级：priority %d", *restoreTo)
		if cfg.CodexKeeper.DryRun {
			message = "模拟" + message
			return &keeperPriorityPolicyAction{Message: message, Result: "priority_restored", Priority: restoreTo}
		}
		if err := a.setKeeperRemotePriority(ctx, cfg, name, restoreTo); err != nil {
			message = "恢复优先级失败：" + err.Error()
			return &keeperPriorityPolicyAction{Message: message}
		}
		return &keeperPriorityPolicyAction{Message: message, Result: "priority_restored", Priority: restoreTo}
	}
	if currentPriority < -1 || currentPriority > 20 {
		return nil
	}
	if next == nil {
		return nil
	}
	if currentPriority != *next {
		message := fmt.Sprintf("应用类型优先级：%s -> priority %d", valueOr(accountType, "unknown"), *next)
		if cfg.CodexKeeper.DryRun {
			message = "模拟" + message
			return &keeperPriorityPolicyAction{Message: message, Result: "priority_restored", Priority: next}
		}
		if err := a.setKeeperRemotePriority(ctx, cfg, name, next); err != nil {
			message = "写入类型优先级失败：" + err.Error()
			return &keeperPriorityPolicyAction{Message: message}
		}
		return &keeperPriorityPolicyAction{Message: message, Result: "priority_restored", Priority: next}
	}
	return nil
}

func keeperEffectivePriority(priority *int) int {
	if priority == nil {
		return 0
	}
	return *priority
}

func keeperDisplayPriority(priority *int) *int {
	if priority != nil {
		return priority
	}
	zero := 0
	return &zero
}

func (a *App) mergeKeeperStats(stats *keeperStats, result keeperAccountResult) {
	switch result.Result {
	case "healthy":
		stats.Healthy++
	case "status_disabled":
		stats.StatusDisabled++
	case "status_enabled":
		stats.StatusEnabled++
	case "priority_degraded":
		stats.PriorityDegraded++
	case "priority_restored":
		stats.PriorityRestored++
	case "network_error":
		stats.NetworkError++
	default:
		stats.Skipped++
	}
}

func (stats *keeperStats) add(delta keeperStats) {
	stats.Total += delta.Total
	stats.Healthy += delta.Healthy
	stats.StatusDisabled += delta.StatusDisabled
	stats.StatusEnabled += delta.StatusEnabled
	stats.PriorityDegraded += delta.PriorityDegraded
	stats.PriorityRestored += delta.PriorityRestored
	stats.Skipped += delta.Skipped
	stats.NetworkError += delta.NetworkError
}

func (stats *keeperStats) mergeCachedState(state keeperAuthState) {
	if isKeeperCachedBadCredentialState(state) {
		stats.StatusDisabled++
		return
	}
	if state.Disabled {
		return
	}
	if state.LastError != nil && strings.TrimSpace(*state.LastError) != "" {
		stats.NetworkError++
		return
	}
	if state.Priority != nil && *state.Priority == -1 {
		stats.PriorityDegraded++
		return
	}
	if state.LastHealthyAt != nil || state.LastCheckedAt != nil {
		stats.Healthy++
	}
}

func isKeeperCachedBadCredentialState(state keeperAuthState) bool {
	if isKeeperBadCredentialDisableAction(state.LatestAction) {
		return true
	}
	if state.LastStatusCode != nil && (*state.LastStatusCode == http.StatusUnauthorized || *state.LastStatusCode == http.StatusPaymentRequired) {
		return true
	}
	if state.LatestAction != nil && strings.HasPrefix(strings.TrimSpace(*state.LatestAction), "模拟禁用") {
		return true
	}
	if state.LastError == nil {
		return false
	}
	message := strings.TrimSpace(*state.LastError)
	return strings.Contains(message, "缺少 access token") || strings.Contains(message, "凭证不可用")
}

func (a *App) listKeeperRemoteAuthFiles(ctx context.Context, cfg AppConfig) ([]map[string]any, error) {
	_, payload, err := a.keeperRequest(ctx, cfg, http.MethodGet, "/v0/management/auth-files", nil, nil, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, validationError("读取认证文件失败：响应不是有效 JSON")
	}
	return extractKeeperObjects(raw, []string{"files", "items", "data", "value"}), nil
}

func (a *App) listKeeperRemoteAuthFileModels(ctx context.Context, cfg AppConfig, name string) ([]map[string]any, error) {
	query := url.Values{"name": []string{name}}
	_, payload, err := a.keeperRequest(
		ctx,
		cfg,
		http.MethodGet,
		"/v0/management/auth-files/models",
		query,
		nil,
		time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second,
	)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, validationError("读取凭证支持模型失败：响应不是有效 JSON")
	}
	items := extractKeeperObjects(raw, []string{"models", "items", "data", "value"})
	models := make([]map[string]any, 0, len(items))
	for _, item := range items {
		id := keeperString(item["id"])
		if id == "" {
			continue
		}
		model := map[string]any{"id": id}
		for _, key := range []string{"display_name", "type", "owned_by"} {
			if value := keeperString(item[key]); value != "" {
				model[key] = value
			}
		}
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool {
		return strings.ToLower(keeperString(models[i]["id"])) < strings.ToLower(keeperString(models[j]["id"]))
	})
	return models, nil
}

type keeperAuthFileUploadFailure struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

type keeperAuthFileUploadCandidate struct {
	Name    string
	Content []byte
}

type keeperAuthFileEditorDetail struct {
	JSON          map[string]any `json:"json"`
	RawText       string         `json:"raw_text,omitempty"`
	InvalidReason string         `json:"invalid_reason,omitempty"`
}

func (a *App) startKeeperOAuth(w http.ResponseWriter, r *http.Request) error {
	request := keeperOAuthStartRequest{Provider: "codex"}
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &request); err != nil {
			return validationError("OAuth 登录参数无效")
		}
	}
	request.ProjectID = strings.TrimSpace(request.ProjectID)
	if len(request.ProjectID) > keeperMaxOAuthProjectIDLength || strings.IndexFunc(request.ProjectID, unicode.IsControl) >= 0 {
		return validationError("OAuth 项目 ID 无效")
	}
	provider, endpoint, err := keeperOAuthProvider(request.Provider)
	if err != nil {
		return err
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	query := url.Values{}
	if keeperOAuthSupportsWebUI(provider) {
		query.Set("is_webui", "true")
	}
	if provider == "gemini-cli" && request.ProjectID != "" {
		query.Set("project_id", request.ProjectID)
	}
	_, payload, err := a.keeperRequest(
		r.Context(),
		cfg,
		http.MethodGet,
		"/v0/management/"+endpoint,
		query,
		nil,
		time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second,
	)
	if err != nil {
		return err
	}
	response := keeperOAuthStartResponse{}
	if err := json.Unmarshal(payload, &response); err != nil {
		return validationError("启动 OAuth 失败：响应不是有效 JSON")
	}
	response.URL = strings.TrimSpace(response.URL)
	response.State = strings.TrimSpace(response.State)
	if err := validateKeeperOAuthURL(response.URL, "OAuth 授权地址"); err != nil {
		return err
	}
	if err := validateKeeperOAuthState(response.State); err != nil {
		return validationError("启动 OAuth 失败：" + err.Error())
	}
	writeJSON(w, http.StatusOK, response)
	return nil
}

func (a *App) getKeeperOAuthStatus(w http.ResponseWriter, r *http.Request) error {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if err := validateKeeperOAuthState(state); err != nil {
		return err
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	query := url.Values{"state": []string{state}}
	_, payload, err := a.keeperRequest(
		r.Context(),
		cfg,
		http.MethodGet,
		"/v0/management/get-auth-status",
		query,
		nil,
		time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second,
	)
	if err != nil {
		return err
	}
	response := keeperOAuthStatusResponse{}
	if err := json.Unmarshal(payload, &response); err != nil {
		return validationError("查询 OAuth 状态失败：响应不是有效 JSON")
	}
	response.Status = strings.TrimSpace(response.Status)
	response.Error = strings.TrimSpace(response.Error)
	if response.Status == "" {
		return validationError("查询 OAuth 状态失败：响应缺少 status")
	}
	writeJSON(w, http.StatusOK, response)
	return nil
}

func (a *App) submitKeeperOAuthCallback(w http.ResponseWriter, r *http.Request) error {
	request := keeperOAuthCallbackRequest{}
	if err := decodeJSON(r, &request); err != nil {
		return err
	}
	request.RedirectURL = strings.TrimSpace(request.RedirectURL)
	provider, _, err := keeperOAuthProvider(request.Provider)
	if err != nil {
		return err
	}
	if err := validateKeeperOAuthURL(request.RedirectURL, "回调 URL"); err != nil {
		return err
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	_, _, err = a.keeperRequest(
		r.Context(),
		cfg,
		http.MethodPost,
		"/v0/management/oauth-callback",
		nil,
		map[string]string{
			"provider":     keeperOAuthCallbackProvider(provider),
			"redirect_url": request.RedirectURL,
		},
		time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second,
	)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "submitted"})
	return nil
}

func keeperOAuthProvider(value string) (string, string, error) {
	provider := strings.ToLower(strings.TrimSpace(value))
	if provider == "" {
		provider = "codex"
	}
	endpoints := map[string]string{
		"codex":       "codex-auth-url",
		"anthropic":   "anthropic-auth-url",
		"antigravity": "antigravity-auth-url",
		"gemini-cli":  "gemini-cli-auth-url",
		"kimi":        "kimi-auth-url",
		"xai":         "xai-auth-url",
	}
	endpoint, ok := endpoints[provider]
	if !ok {
		return "", "", validationError("不支持的 OAuth 提供商")
	}
	return provider, endpoint, nil
}

func keeperOAuthCallbackProvider(provider string) string {
	if provider == "gemini-cli" {
		return "gemini"
	}
	return provider
}

func keeperOAuthSupportsWebUI(provider string) bool {
	return provider != "kimi"
}

func validateKeeperOAuthState(state string) error {
	if state == "" {
		return validationError("OAuth state 不能为空")
	}
	if len(state) > keeperMaxOAuthStateLength {
		return validationError("OAuth state 过长")
	}
	return nil
}

func validateKeeperOAuthURL(rawURL, fieldName string) error {
	if rawURL == "" {
		return validationError(fieldName + "不能为空")
	}
	if len(rawURL) > keeperMaxOAuthURLLength {
		return validationError(fieldName + "过长")
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return validationError(fieldName + "无效")
	}
	return nil
}

func (a *App) uploadKeeperAuthFiles(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return validationError("上传认证文件失败：请求不是有效的 multipart 表单")
	}
	if r.MultipartForm == nil {
		return validationError("请选择要上传的认证文件")
	}
	defer r.MultipartForm.RemoveAll()
	fileHeaders := r.MultipartForm.File["file"]
	if len(fileHeaders) == 0 {
		fileHeaders = r.MultipartForm.File["files"]
	}
	if len(fileHeaders) == 0 {
		return validationError("请选择要上传的认证文件")
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	uploaded := make([]string, 0, len(fileHeaders))
	failed := make([]keeperAuthFileUploadFailure, 0)
	for _, header := range fileHeaders {
		name := strings.TrimSpace(header.Filename)
		if name == "" || strings.ContainsAny(name, `/\\`) {
			failed = append(failed, keeperAuthFileUploadFailure{Name: name, Error: "认证文件名称无效"})
			continue
		}
		if !strings.EqualFold(filepath.Ext(name), ".json") {
			failed = append(failed, keeperAuthFileUploadFailure{Name: name, Error: "只能上传 JSON 文件"})
			continue
		}
		if header.Size > keeperMaxAuthFileSize {
			failed = append(failed, keeperAuthFileUploadFailure{Name: name, Error: "文件大小不能超过 10 MB"})
			continue
		}
		file, err := header.Open()
		if err != nil {
			failed = append(failed, keeperAuthFileUploadFailure{Name: name, Error: "无法读取上传文件"})
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(file, keeperMaxAuthFileSize+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			failed = append(failed, keeperAuthFileUploadFailure{Name: name, Error: "无法读取上传文件"})
			continue
		}
		if len(content) > keeperMaxAuthFileSize {
			failed = append(failed, keeperAuthFileUploadFailure{Name: name, Error: "文件大小不能超过 10 MB"})
			continue
		}
		candidates, conversionFailures := keeperAuthFileUploadCandidates(name, content, time.Now().In(appTimeLocation))
		failed = append(failed, conversionFailures...)
		for _, candidate := range candidates {
			if len(candidate.Content) > keeperMaxAuthFileSize {
				failed = append(failed, keeperAuthFileUploadFailure{Name: candidate.Name, Error: "转换后的认证文件大小不能超过 10 MB"})
				continue
			}
			if err := a.uploadKeeperRemoteAuthFile(r.Context(), cfg, candidate.Name, candidate.Content); err != nil {
				failed = append(failed, keeperAuthFileUploadFailure{Name: candidate.Name, Error: err.Error()})
				continue
			}
			uploaded = append(uploaded, candidate.Name)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"uploaded": len(uploaded),
		"files":    uploaded,
		"failed":   failed,
	})
	return nil
}

func keeperAuthFileUploadCandidates(name string, content []byte, now time.Time) ([]keeperAuthFileUploadCandidate, []keeperAuthFileUploadFailure) {
	accounts, recognized, err := keeperSub2APIAccounts(content)
	if !recognized {
		return []keeperAuthFileUploadCandidate{{Name: name, Content: content}}, nil
	}
	if err != nil {
		return nil, []keeperAuthFileUploadFailure{{Name: name, Error: err.Error()}}
	}
	usedNames := map[string]int{}
	candidates := []keeperAuthFileUploadCandidate{}
	failures := []keeperAuthFileUploadFailure{}
	matched := 0
	for index, account := range accounts {
		platform := strings.ToLower(keeperString(account["platform"]))
		authType := strings.ToLower(keeperString(account["type"]))
		if platform != "openai" || authType != "oauth" {
			continue
		}
		matched++
		converted, err := keeperSub2APIAccountToCPA(account, now)
		failureName := fmt.Sprintf("%s#accounts[%d]", name, index)
		if err != nil {
			failures = append(failures, keeperAuthFileUploadFailure{Name: failureName, Error: err.Error()})
			continue
		}
		convertedContent, err := json.MarshalIndent(converted, "", "  ")
		if err != nil {
			failures = append(failures, keeperAuthFileUploadFailure{Name: failureName, Error: "转换 Sub2API 凭证失败：" + err.Error()})
			continue
		}
		candidateName := keeperSub2APIAccountFileName(account, converted, index, usedNames)
		candidates = append(candidates, keeperAuthFileUploadCandidate{Name: candidateName, Content: convertedContent})
	}
	if matched == 0 {
		failures = append(failures, keeperAuthFileUploadFailure{Name: name, Error: "Sub2API 文件中没有可转换的 OpenAI OAuth 凭证"})
	}
	return candidates, failures
}

func keeperSub2APIAccounts(content []byte) ([]map[string]any, bool, error) {
	var document map[string]any
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, false, nil
	}
	rawAccounts, hasAccounts := document["accounts"]
	if !hasAccounts {
		return nil, false, nil
	}
	_, hasExportedAt := document["exported_at"]
	_, hasProxies := document["proxies"]
	accountValues, isArray := rawAccounts.([]any)
	recognized := hasExportedAt || hasProxies
	if !recognized && isArray {
		for _, value := range accountValues {
			account, ok := value.(map[string]any)
			if !ok {
				continue
			}
			_, hasCredentials := account["credentials"]
			if strings.EqualFold(keeperString(account["platform"]), "openai") && hasCredentials {
				recognized = true
				break
			}
		}
	}
	if !recognized {
		return nil, false, nil
	}
	if !isArray {
		return nil, true, validationError("Sub2API accounts 必须是数组")
	}
	if len(accountValues) == 0 {
		return nil, true, validationError("Sub2API accounts 不能为空")
	}
	accounts := make([]map[string]any, 0, len(accountValues))
	for index, value := range accountValues {
		account, ok := value.(map[string]any)
		if !ok {
			return nil, true, validationError(fmt.Sprintf("Sub2API accounts[%d] 必须是对象", index))
		}
		accounts = append(accounts, account)
	}
	return accounts, true, nil
}

func keeperSub2APIAccountToCPA(account map[string]any, now time.Time) (map[string]any, error) {
	credentials, ok := account["credentials"].(map[string]any)
	if !ok {
		return nil, validationError("Sub2API 凭证缺少 credentials")
	}
	extra, _ := account["extra"].(map[string]any)
	accessToken := keeperFirstString(
		credentials["access_token"],
		credentials["accessToken"],
		account["access_token"],
		account["accessToken"],
	)
	if accessToken == "" {
		return nil, validationError("Sub2API 凭证缺少 access_token")
	}
	refreshToken := keeperFirstString(credentials["refresh_token"], credentials["refreshToken"], account["refresh_token"], account["refreshToken"])
	idToken := keeperFirstString(credentials["id_token"], credentials["idToken"], account["id_token"], account["idToken"])
	sessionToken := keeperFirstString(credentials["session_token"], credentials["sessionToken"], account["session_token"], account["sessionToken"])
	accessClaims := keeperIDTokenClaims(accessToken)
	idClaims := keeperIDTokenClaims(idToken)
	accessAuth := keeperClaimObject(accessClaims, "https://api.openai.com/auth")
	idAuth := keeperClaimObject(idClaims, "https://api.openai.com/auth")
	accessProfile := keeperClaimObject(accessClaims, "https://api.openai.com/profile")
	email := keeperFirstString(
		account["email"],
		credentials["email"],
		extra["email"],
		accessProfile["email"],
		keeperClaimValue(idClaims, "email"),
		keeperClaimValue(accessClaims, "email"),
	)
	accountID := keeperFirstString(
		account["account_id"],
		account["chatgpt_account_id"],
		credentials["account_id"],
		credentials["chatgpt_account_id"],
		credentials["chatgptAccountId"],
		accessAuth["chatgpt_account_id"],
		idAuth["chatgpt_account_id"],
	)
	userID := keeperFirstString(
		account["user_id"],
		account["chatgpt_user_id"],
		credentials["user_id"],
		credentials["chatgpt_user_id"],
		credentials["chatgptUserId"],
		accessAuth["chatgpt_user_id"],
		accessAuth["user_id"],
		idAuth["chatgpt_user_id"],
		idAuth["user_id"],
	)
	planType := keeperFirstString(
		account["plan_type"],
		account["planType"],
		credentials["plan_type"],
		credentials["planType"],
		accessAuth["chatgpt_plan_type"],
		idAuth["chatgpt_plan_type"],
	)
	accountName := keeperFirstString(email, account["name"], accountID)
	expiresAt := keeperSub2APIExpiration(account, credentials, accessClaims, refreshToken == "")
	syntheticIDToken := false
	if idToken == "" && accountID != "" {
		var err error
		idToken, err = keeperSyntheticCodexIDToken(email, accountID, planType, userID, expiresAt, now)
		if err != nil {
			return nil, validationError("构造 CPA id_token 失败：" + err.Error())
		}
		syntheticIDToken = true
	}
	converted := map[string]any{
		"type":          "codex",
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"last_refresh":  keeperISOTime(now),
	}
	keeperSetIfNotEmpty(converted, "account_id", accountID)
	keeperSetIfNotEmpty(converted, "chatgpt_account_id", accountID)
	keeperSetIfNotEmpty(converted, "email", email)
	keeperSetIfNotEmpty(converted, "name", accountName)
	keeperSetIfNotEmpty(converted, "plan_type", planType)
	keeperSetIfNotEmpty(converted, "chatgpt_plan_type", planType)
	keeperSetIfNotEmpty(converted, "id_token", idToken)
	keeperSetIfNotEmpty(converted, "session_token", sessionToken)
	if syntheticIDToken {
		converted["id_token_synthetic"] = true
	}
	if expiresAt != nil {
		converted["expired"] = keeperISOTime(*expiresAt)
	}
	if keeperBool(account["disabled"]) {
		converted["disabled"] = true
	}
	return converted, nil
}

func keeperSub2APIExpiration(account, credentials, accessClaims map[string]any, include bool) *time.Time {
	if !include {
		return nil
	}
	if expiresAt := keeperTimeFromUnixSeconds(keeperClaimValue(accessClaims, "exp")); expiresAt != nil {
		return expiresAt
	}
	for _, value := range []any{account["expires"], account["expires_at"], account["expiresAt"], credentials["expires_at"], credentials["expiresAt"]} {
		if expiresAt := keeperTimeValue(value); expiresAt != nil {
			return expiresAt
		}
	}
	return nil
}

func keeperSyntheticCodexIDToken(email, accountID, planType, userID string, expiresAt *time.Time, now time.Time) (string, error) {
	authInfo := map[string]any{"chatgpt_account_id": accountID}
	keeperSetIfNotEmpty(authInfo, "chatgpt_plan_type", planType)
	if userID != "" {
		authInfo["chatgpt_user_id"] = userID
		authInfo["user_id"] = userID
	}
	expires := now.Add(90 * 24 * time.Hour).Unix()
	if expiresAt != nil {
		expires = expiresAt.Unix()
	}
	payload := map[string]any{
		"iat":                         now.Unix(),
		"exp":                         expires,
		"https://api.openai.com/auth": authInfo,
	}
	keeperSetIfNotEmpty(payload, "email", email)
	headerJSON, err := json.Marshal(map[string]any{"alg": "none", "typ": "JWT", "cpa_synthetic": true})
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON) + ".synthetic", nil
}

func keeperSub2APIAccountFileName(account, converted map[string]any, index int, usedNames map[string]int) string {
	credentials, _ := account["credentials"].(map[string]any)
	identity := keeperFirstString(converted["email"], account["name"], converted["account_id"], credentials["chatgpt_account_id"])
	token := keeperAuthFileNameToken(identity)
	if token == "" {
		token = fmt.Sprintf("account-%d", index+1)
	}
	base := "codex-" + token
	key := strings.ToLower(base)
	usedNames[key]++
	if usedNames[key] > 1 {
		base += fmt.Sprintf("-%d", usedNames[key])
	}
	return base + ".json"
}

func keeperAuthFileNameToken(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	runes := []rune{}
	lastDash := false
	for _, char := range value {
		if unicode.IsControl(char) || unicode.IsSpace(char) || strings.ContainsRune(`/\\:*?"<>|`, char) {
			if !lastDash {
				runes = append(runes, '-')
				lastDash = true
			}
			continue
		}
		runes = append(runes, char)
		lastDash = false
		if len(runes) >= 100 {
			break
		}
	}
	return strings.Trim(string(runes), ".-_")
}

func keeperFirstString(values ...any) string {
	for _, value := range values {
		if text := keeperString(value); text != "" {
			return text
		}
	}
	return ""
}

func keeperClaimObject(claims map[string]any, key string) map[string]any {
	if claims == nil {
		return nil
	}
	object, _ := claims[key].(map[string]any)
	return object
}

func keeperClaimValue(claims map[string]any, key string) any {
	if claims == nil {
		return nil
	}
	return claims[key]
}

func keeperSetIfNotEmpty(target map[string]any, key, value string) {
	if value != "" {
		target[key] = value
	}
}

func keeperTimeFromUnixSeconds(value any) *time.Time {
	var seconds int64
	switch typed := value.(type) {
	case float64:
		seconds = int64(typed)
	case float32:
		seconds = int64(typed)
	case int:
		seconds = int64(typed)
	case int64:
		seconds = typed
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return nil
		}
		seconds = parsed
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return nil
		}
		seconds = parsed
	default:
		return nil
	}
	if seconds <= 0 {
		return nil
	}
	if seconds > 100_000_000_000 {
		seconds /= 1000
	}
	result := time.Unix(seconds, 0).UTC()
	return &result
}

func keeperTimeValue(value any) *time.Time {
	if value == nil {
		return nil
	}
	if parsed := keeperTimeFromUnixSeconds(value); parsed != nil {
		return parsed
	}
	text := keeperString(value)
	if text == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return nil
	}
	result := parsed.UTC()
	return &result
}

func keeperISOTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}

func (a *App) uploadKeeperRemoteAuthFile(ctx context.Context, cfg AppConfig, name string, content []byte) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		return validationError("创建认证文件上传请求失败：" + err.Error())
	}
	if _, err := part.Write(content); err != nil {
		return validationError("创建认证文件上传请求失败：" + err.Error())
	}
	if err := writer.Close(); err != nil {
		return validationError("创建认证文件上传请求失败：" + err.Error())
	}
	_, _, err = a.keeperRawRequest(
		ctx,
		cfg,
		http.MethodPost,
		"/v0/management/auth-files",
		nil,
		body.Bytes(),
		writer.FormDataContentType(),
		time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second,
		8<<20,
	)
	return err
}

func (a *App) getKeeperRemoteAuthFileEditorDetail(ctx context.Context, cfg AppConfig, name string) (*keeperAuthFileEditorDetail, error) {
	payload, err := a.downloadKeeperRemoteAuthFile(ctx, cfg, name)
	if err != nil || payload == nil {
		return nil, err
	}
	authFiles, err := a.listKeeperRemoteAuthFiles(ctx, cfg)
	if err != nil {
		return nil, err
	}
	note := ""
	for _, authFile := range authFiles {
		if keeperString(authFile["name"]) == name {
			note = keeperString(authFile["note"])
			break
		}
	}
	var raw any
	if err := json.Unmarshal(payload, &raw); err == nil {
		if object, ok := raw.(map[string]any); ok {
			// CPA stores note as auth-file metadata returned by the list API rather
			// than in the downloadable JSON. Keep CPA as the only source of truth.
			delete(object, "note")
			if note != "" {
				object["note"] = note
			}
			return &keeperAuthFileEditorDetail{JSON: object}, nil
		}
	}
	reason := "invalid_json"
	if keeperLooksLikeHTMLChallenge(payload) {
		reason = "html_challenge"
	}
	return &keeperAuthFileEditorDetail{
		RawText:       string(payload),
		InvalidReason: reason,
	}, nil
}

func keeperLooksLikeHTMLChallenge(payload []byte) bool {
	text := strings.ToLower(strings.TrimSpace(string(payload)))
	if len(text) > 4096 {
		text = text[:4096]
	}
	return strings.HasPrefix(text, "<!doctype html") ||
		strings.HasPrefix(text, "<html") ||
		strings.Contains(text, "<head") ||
		strings.Contains(text, "<body") ||
		strings.Contains(text, "cf_chl") ||
		strings.Contains(text, "__cf_chl_tk") ||
		strings.Contains(text, "challenge-platform") ||
		strings.Contains(text, "cloudflare")
}

func (a *App) downloadKeeperRemoteAuthFile(ctx context.Context, cfg AppConfig, name string) ([]byte, error) {
	query := url.Values{"name": []string{name}}
	response, payload, err := a.keeperRawRequest(
		ctx,
		cfg,
		http.MethodGet,
		"/v0/management/auth-files/download",
		query,
		nil,
		"",
		time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second,
		keeperMaxAuthFileSize,
	)
	if response != nil && response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	return payload, err
}

func (a *App) getKeeperRemoteAuthFile(ctx context.Context, cfg AppConfig, name string) (map[string]any, error) {
	payload, err := a.downloadKeeperRemoteAuthFile(ctx, cfg, name)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, validationError("读取认证文件详情失败：响应不是有效 JSON")
	}
	return raw, nil
}

func (a *App) setKeeperRemoteDisabled(ctx context.Context, cfg AppConfig, name string, disabled bool) error {
	_, _, err := a.keeperRequest(ctx, cfg, http.MethodPatch, "/v0/management/auth-files/status", nil, map[string]any{"name": name, "disabled": disabled}, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	return err
}

func (a *App) updateKeeperRemoteAuthFileFields(ctx context.Context, cfg AppConfig, name string, fields map[string]any) error {
	payload := map[string]any{"name": name}
	for key, value := range fields {
		switch key {
		case "prefix", "proxy_url", "note":
			text, ok := value.(string)
			if !ok {
				return validationError(fmt.Sprintf("认证文件字段 %s 必须是字符串", key))
			}
			payload[key] = text
		case "priority", "weight", "request_retry":
			if value == nil && (key == "weight" || key == "request_retry") {
				payload[key] = nil
				continue
			}
			number, ok := value.(float64)
			if !ok || number != math.Trunc(number) {
				return validationError(fmt.Sprintf("认证文件字段 %s 必须是整数", key))
			}
			if number < -1000000 || number > 1000000 {
				return validationError(fmt.Sprintf("认证文件字段 %s 超出允许范围", key))
			}
			payload[key] = int(number)
		case "websockets", "using_api":
			enabled, ok := value.(bool)
			if !ok {
				return validationError(fmt.Sprintf("认证文件字段 %s 必须是布尔值", key))
			}
			payload[key] = enabled
		case "headers":
			headers, ok := value.(map[string]any)
			if !ok {
				return validationError("认证文件 headers 必须是 JSON 对象")
			}
			normalized := map[string]string{}
			for headerName, headerValue := range headers {
				text, ok := headerValue.(string)
				if !ok {
					return validationError("认证文件 headers 的值必须是字符串")
				}
				normalized[headerName] = text
			}
			payload[key] = normalized
		default:
			return validationError("认证文件包含不支持的字段")
		}
	}
	if len(payload) == 1 {
		return validationError("没有需要更新的认证文件字段")
	}
	_, _, err := a.keeperRequest(ctx, cfg, http.MethodPatch, "/v0/management/auth-files/fields", nil, payload, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	return err
}

func (a *App) setKeeperRemotePriority(ctx context.Context, cfg AppConfig, name string, priority *int) error {
	_, _, err := a.keeperRequest(ctx, cfg, http.MethodPatch, "/v0/management/auth-files/fields", nil, map[string]any{"name": name, "priority": priority}, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	return err
}

func (a *App) setKeeperRemoteWebsockets(ctx context.Context, cfg AppConfig, name string) error {
	_, _, err := a.keeperRequest(ctx, cfg, http.MethodPatch, "/v0/management/auth-files/fields", nil, map[string]any{"name": name, "websockets": true}, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	return err
}

func (a *App) deleteKeeperRemoteAuthFile(ctx context.Context, cfg AppConfig, name string) error {
	query := url.Values{"name": []string{name}}
	_, _, err := a.keeperRequest(ctx, cfg, http.MethodDelete, "/v0/management/auth-files", query, nil, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	return err
}

func (a *App) keeperRequest(ctx context.Context, cfg AppConfig, method, path string, query url.Values, body any, timeout time.Duration) (*http.Response, []byte, error) {
	attempts := keeperRequestAttempts(cfg.CodexKeeper)
	target := makeURL(cfg.Collector.CLIProxyURL, path, query)
	headers := managementHeaders(cfg.Collector.ManagementKey)
	var lastResponse *http.Response
	var lastPayload []byte
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		response, payload, err := doJSON(ctx, httpClient(timeout), method, target, headers, body)
		lastResponse = response
		lastPayload = payload
		if err != nil {
			lastErr = validationError("CLIProxyAPI 管理请求失败：" + err.Error())
			if keeperShouldRetryRequest(ctx, attempt, attempts, response, err) {
				continue
			}
			return nil, nil, lastErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastErr = validationError(fmt.Sprintf("CLIProxyAPI 管理请求失败：HTTP %d", response.StatusCode))
			if keeperShouldRetryRequest(ctx, attempt, attempts, response, nil) {
				continue
			}
			return response, payload, lastErr
		}
		return response, payload, nil
	}
	return lastResponse, lastPayload, lastErr
}

func (a *App) keeperRawRequest(ctx context.Context, cfg AppConfig, method, path string, query url.Values, body []byte, contentType string, timeout time.Duration, responseLimit int64) (*http.Response, []byte, error) {
	attempts := keeperRequestAttempts(cfg.CodexKeeper)
	target := makeURL(cfg.Collector.CLIProxyURL, path, query)
	headers := managementHeaders(cfg.Collector.ManagementKey)
	var lastResponse *http.Response
	var lastPayload []byte
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		request, err := http.NewRequestWithContext(ctx, method, target, reader)
		if err != nil {
			return nil, nil, validationError("CLIProxyAPI 管理请求失败：" + err.Error())
		}
		for key, values := range headers {
			for _, value := range values {
				request.Header.Add(key, value)
			}
		}
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		response, requestErr := httpClient(timeout).Do(request)
		lastResponse = response
		if requestErr != nil {
			lastErr = validationError("CLIProxyAPI 管理请求失败：" + requestErr.Error())
			if keeperShouldRetryRequest(ctx, attempt, attempts, response, requestErr) {
				continue
			}
			return nil, nil, lastErr
		}
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
		closeErr := response.Body.Close()
		lastPayload = payload
		if readErr != nil || closeErr != nil {
			if readErr == nil {
				readErr = closeErr
			}
			lastErr = validationError("CLIProxyAPI 管理请求失败：" + readErr.Error())
			if keeperShouldRetryRequest(ctx, attempt, attempts, response, readErr) {
				continue
			}
			return response, payload, lastErr
		}
		if int64(len(payload)) > responseLimit {
			return response, payload[:responseLimit], validationError("CLIProxyAPI 管理响应超过大小限制")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastErr = validationError(fmt.Sprintf("CLIProxyAPI 管理请求失败：HTTP %d", response.StatusCode))
			if keeperShouldRetryRequest(ctx, attempt, attempts, response, nil) {
				continue
			}
			return response, payload, lastErr
		}
		return response, payload, nil
	}
	return lastResponse, lastPayload, lastErr
}

func keeperRequestAttempts(cfg KeeperConfig) int {
	return clampInt(cfg.MaxRetries, 0, 5, 2) + 1
}

func keeperShouldRetryRequest(ctx context.Context, attempt, attempts int, response *http.Response, err error) bool {
	if attempt >= attempts || ctx.Err() != nil {
		return false
	}
	if err != nil {
		return true
	}
	return response != nil && response.StatusCode >= 500
}

func (a *App) queryKeeperResetCredits(ctx context.Context, authName string) (*keeperResetCreditsResponse, error) {
	cfg, detail, err := a.keeperResetCreditsAccount(ctx, authName)
	if err != nil {
		return nil, err
	}
	result, err := a.fetchKeeperResetCredits(ctx, cfg, detail)
	if err != nil {
		return nil, err
	}
	a.storeKeeperResetCredits(authName, *result)
	return result, nil
}

func (a *App) consumeKeeperResetCredit(ctx context.Context, authName string) (*keeperResetCreditsResponse, error) {
	cfg, detail, err := a.keeperResetCreditsAccount(ctx, authName)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]string{"redeem_request_id": uuid.NewString()})
	if err != nil {
		return nil, err
	}
	if _, err := a.keeperResetCreditsAPICall(ctx, cfg, detail, http.MethodPost, keeperResetCreditsConsumeURL, string(body), "使用重置次数"); err != nil {
		return nil, err
	}

	// A successful redemption changes the upstream value, so an older cached
	// count must never be returned if the follow-up query fails.
	a.deleteCachedKeeperResetCredits(authName)
	result, err := a.fetchKeeperResetCredits(ctx, cfg, detail)
	if err != nil {
		return nil, validationError("已使用重置次数，但重新查询剩余次数失败：" + err.Error())
	}
	a.storeKeeperResetCredits(authName, *result)
	if a.keeper != nil {
		if err := a.keeper.StartAccounts([]string{authName}); err != nil {
			slog.Warn("start account inspection after reset credit consumption", "auth_name", authName, "error", err)
		}
	}
	return result, nil
}

func (a *App) keeperResetCreditsAccount(ctx context.Context, authName string) (AppConfig, map[string]any, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return AppConfig{}, nil, err
	}
	state, err := a.getKeeperState(ctx, authName)
	if err != nil {
		return AppConfig{}, nil, err
	}
	detail, err := a.getKeeperRemoteAuthFile(ctx, cfg, state.Name)
	if err != nil {
		return AppConfig{}, nil, err
	}
	if detail == nil {
		return AppConfig{}, nil, notFoundError("认证文件不存在")
	}
	if keeperRemoteAuthIndex(detail) == nil && state.AuthIndex != nil {
		detail["auth_index"] = *state.AuthIndex
	}
	if keeperRemoteAuthIndex(detail) == nil {
		return AppConfig{}, nil, validationError("认证文件缺少 auth_index，无法查询重置次数")
	}
	return cfg, detail, nil
}

func (a *App) fetchKeeperResetCredits(ctx context.Context, cfg AppConfig, detail map[string]any) (*keeperResetCreditsResponse, error) {
	body, err := a.keeperResetCreditsAPICall(ctx, cfg, detail, http.MethodGet, keeperResetCreditsURL, "", "查询重置次数")
	if err != nil {
		return nil, err
	}
	result := keeperResetCreditsResponseFromBody(body, time.Now())
	return &result, nil
}

func keeperResetCreditsResponseFromBody(body map[string]any, cachedAt time.Time) keeperResetCreditsResponse {
	return keeperResetCreditsResponse{
		AvailableCount:   keeperResetCreditsAvailableCount(body),
		TotalEarnedCount: keeperIntPtr(body["total_earned_count"], body["totalEarnedCount"]),
		EarliestExpires:  keeperResetCreditsEarliestExpiry(body),
		CachedAt:         apiDateTime(cachedAt),
	}
}

func keeperEmbeddedResetCredits(body map[string]any) map[string]any {
	if body == nil {
		return nil
	}
	for _, key := range []string{"rate_limit_reset_credits", "rateLimitResetCredits"} {
		if object, ok := body[key].(map[string]any); ok {
			return object
		}
	}
	return nil
}

func (a *App) keeperResetCreditsAPICall(
	ctx context.Context,
	cfg AppConfig,
	detail map[string]any,
	method string,
	targetURL string,
	data string,
	action string,
) (map[string]any, error) {
	header := map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Accept":        "application/json",
		"Content-Type":  "application/json",
		"OpenAI-Beta":   "codex-1",
		"Originator":    "Codex Desktop",
		"User-Agent":    "codex_cli_rs/0.76.0",
	}
	if accountID := keeperStringPtr(detail["account_id"], detail["accountId"]); accountID != nil {
		header["Chatgpt-Account-Id"] = *accountID
	}
	requestBody := map[string]any{
		"auth_index": keeperAuthIndex(detail),
		"method":     method,
		"url":        targetURL,
		"header":     header,
		"data":       data,
	}
	_, payload, err := a.keeperRequest(
		ctx,
		cfg,
		http.MethodPost,
		"/v0/management/api-call",
		nil,
		requestBody,
		time.Duration(cfg.CodexKeeper.UsageTimeoutSeconds)*time.Second,
	)
	if err != nil {
		return nil, validationError(action + "失败：" + err.Error())
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, validationError(action + "失败：api-call 响应不是有效 JSON")
	}
	statusCode := keeperIntPtr(raw["status_code"], raw["statusCode"])
	if statusCode == nil {
		return nil, validationError(action + "失败：api-call 响应缺少 status_code")
	}
	if *statusCode < http.StatusOK || *statusCode >= http.StatusMultipleChoices {
		brief := briefAny(raw["body"])
		if brief != "" {
			return nil, validationError(fmt.Sprintf("%s失败：上游 HTTP %d：%s", action, *statusCode, brief))
		}
		return nil, validationError(fmt.Sprintf("%s失败：上游 HTTP %d", action, *statusCode))
	}
	if method == http.MethodPost {
		return map[string]any{}, nil
	}
	bodyJSON := keeperBodyJSON(raw["body"])
	if bodyJSON == nil {
		return nil, validationError(action + "失败：上游响应不是有效 JSON")
	}
	return bodyJSON, nil
}

func keeperResetCreditsAvailableCount(body map[string]any) int {
	if count := keeperIntPtr(body["available_count"], body["availableCount"]); count != nil {
		return max(0, *count)
	}
	count := 0
	for _, credit := range keeperResetCreditObjects(body) {
		status := strings.ToLower(keeperString(credit["status"]))
		if status == "" || status == "available" {
			count++
		}
	}
	return count
}

func keeperResetCreditsEarliestExpiry(body map[string]any) *string {
	var earliest *time.Time
	for _, credit := range keeperResetCreditObjects(body) {
		status := strings.ToLower(keeperString(credit["status"]))
		if status != "" && status != "available" {
			continue
		}
		expiresAt := keeperTimeValue(firstKeeperValue(credit["expires_at"], credit["expiresAt"]))
		if expiresAt != nil && (earliest == nil || expiresAt.Before(*earliest)) {
			earliest = expiresAt
		}
	}
	return apiDateTimePtr(earliest)
}

func keeperResetCreditObjects(body map[string]any) []map[string]any {
	values, ok := body["credits"].([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if object, ok := value.(map[string]any); ok {
			result = append(result, object)
		}
	}
	return result
}

func firstKeeperValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func (a *App) checkKeeperUsage(ctx context.Context, cfg AppConfig, detail map[string]any) keeperHTTPResult {
	authIndex := keeperAuthIndex(detail)
	header := map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"User-Agent":    "codex_cli_rs/0.76.0",
	}
	if accountID := keeperString(detail["account_id"]); accountID != "" {
		header["Chatgpt-Account-Id"] = accountID
	}
	body := map[string]any{
		"auth_index": authIndex,
		"method":     "GET",
		"url":        keeperUsageURL,
		"header":     header,
		"data":       "",
	}
	response, payload, err := a.keeperRequest(ctx, cfg, http.MethodPost, "/v0/management/api-call", nil, body, time.Duration(cfg.CodexKeeper.UsageTimeoutSeconds)*time.Second)
	if err != nil {
		return keeperHTTPResult{Error: err.Error()}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return keeperHTTPResult{Error: fmt.Sprintf("api-call 管理请求失败：HTTP %d", response.StatusCode), Brief: briefPayload(payload)}
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return keeperHTTPResult{Error: "api-call 响应不是有效 JSON"}
	}
	statusCode := keeperIntPtr(raw["status_code"], raw["statusCode"])
	if statusCode == nil {
		return keeperHTTPResult{Error: "api-call 响应缺少 status_code"}
	}
	bodyJSON := keeperBodyJSON(raw["body"])
	return keeperHTTPResult{
		StatusCode: statusCode,
		JSONData:   bodyJSON,
		Brief:      briefAny(raw["body"]),
	}
}

func (a *App) listKeeperCredentials(ctx context.Context) ([]keeperAccount, bool, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return nil, false, err
	}
	authFiles, err := a.listKeeperRemoteAuthFiles(ctx, cfg)
	if err != nil {
		accounts, localErr := a.listKeeperAccounts(ctx)
		if localErr != nil {
			return nil, false, localErr
		}
		for index := range accounts {
			accounts[index].Provider = "codex"
			accounts[index].PlanType = accounts[index].AccountType
		}
		return accounts, true, nil
	}
	inspectedAccounts, err := a.listKeeperAccounts(ctx)
	if err != nil {
		return nil, false, err
	}
	inspectedByName := make(map[string]keeperAccount, len(inspectedAccounts))
	for _, account := range inspectedAccounts {
		inspectedByName[account.Name] = account
	}

	remoteByName := keeperRemoteAuthFilesByName(authFiles)
	names := make([]string, 0, len(remoteByName))
	for name := range remoteByName {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	accounts := make([]keeperAccount, 0, len(names))
	for _, name := range names {
		item := remoteByName[name]
		provider := keeperCredentialProvider(item)
		account := keeperAccount{
			Name:          name,
			Provider:      provider,
			Email:         keeperStringPtr(item["email"]),
			Label:         keeperStringPtr(item["label"]),
			Account:       keeperStringPtr(item["account"]),
			ProjectID:     keeperStringPtr(item["project_id"], item["projectId"]),
			UserID:        keeperStringPtr(item["user_id"], item["userId"]),
			AuthIndex:     keeperRemoteAuthIndex(item),
			AccountType:   keeperStringPtr(item["account_type"], item["accountType"]),
			PlanType:      keeperStringPtr(item["plan_type"], item["planType"]),
			Weight:        keeperIntPtr(item["weight"]),
			RequestRetry:  keeperIntPtr(item["request_retry"], item["request-retry"]),
			Disabled:      keeperBool(item["disabled"]),
			Unavailable:   keeperBool(item["unavailable"]),
			RuntimeOnly:   keeperBool(item["runtime_only"], item["runtimeOnly"]),
			Status:        keeperStringPtr(item["status"]),
			StatusMessage: keeperStringPtr(item["status_message"], item["statusMessage"]),
			Success:       keeperIntPtr(item["success"]),
			Failed:        keeperIntPtr(item["failed"]),
			Quota:         keeperObject(item["quota"]),
			ModelQuotas:   keeperObject(item["model_quotas"], item["modelQuotas"]),
			LastRefreshAt: keeperRemoteTimePtr(item["last_refresh"], item["lastRefresh"], item["last_refreshed_at"]),
			ModifiedAt:    keeperRemoteTimePtr(item["modtime"], item["updated_at"], item["modified"]),
			Priority:      keeperIntPtr(item["priority"]),
		}
		if inspected, ok := inspectedByName[name]; ok && keeperIsCodexProvider(provider) {
			if account.Email == nil {
				account.Email = inspected.Email
			}
			if account.AuthIndex == nil {
				account.AuthIndex = inspected.AuthIndex
			}
			if account.AccountType == nil {
				account.AccountType = inspected.AccountType
			}
			if account.PlanType == nil {
				account.PlanType = inspected.AccountType
			}
			if !keeperMapHasAnyKey(item, "disabled") {
				account.Disabled = inspected.Disabled
			}
			if account.Priority == nil {
				account.Priority = inspected.Priority
			}
			account.PrimaryUsedPercent = inspected.PrimaryUsedPercent
			account.SecondaryUsedPercent = inspected.SecondaryUsedPercent
			account.PrimaryResetAt = inspected.PrimaryResetAt
			account.SecondaryResetAt = inspected.SecondaryResetAt
			account.PrimaryWindowSeconds = inspected.PrimaryWindowSeconds
			account.SecondaryWindowSeconds = inspected.SecondaryWindowSeconds
			account.QuotaThreshold = inspected.QuotaThreshold
			account.LastStatusCode = inspected.LastStatusCode
			account.LastError = inspected.LastError
			account.LatestAction = inspected.LatestAction
			account.LastCheckedAt = inspected.LastCheckedAt
			account.LastHealthyAt = inspected.LastHealthyAt
		}
		accounts = append(accounts, account)
	}
	return accounts, false, nil
}

func keeperCredentialProvider(item map[string]any) string {
	provider := strings.ToLower(keeperFirstString(item["type"], item["provider"]))
	if provider == "" {
		return "unknown"
	}
	return provider
}

func keeperIsCodexProvider(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "codex")
}

func keeperRemoteTimePtr(values ...any) *time.Time {
	for _, value := range values {
		if parsed := keeperTimeValue(value); parsed != nil {
			return parsed
		}
	}
	return nil
}

func keeperRemoteAuthFilesByName(authFiles []map[string]any) map[string]map[string]any {
	// CPA may return a physical auth file together with runtime entries expanded
	// from that file. Auth-file download, field updates, and deletion are all
	// source-file operations keyed by name, so expose one merged file row just as
	// the reference panel does instead of presenting runtime children as files.
	grouped := map[string][]map[string]any{}
	for _, item := range authFiles {
		name := keeperString(item["name"])
		if name == "" {
			continue
		}
		grouped[name] = append(grouped[name], item)
	}
	result := make(map[string]map[string]any, len(grouped))
	for name, items := range grouped {
		sort.SliceStable(items, func(i, j int) bool {
			return keeperRemoteAuthFileScore(items[i]) > keeperRemoteAuthFileScore(items[j])
		})
		merged := map[string]any{}
		for _, item := range items {
			for key, value := range item {
				if !keeperHasMeaningfulValue(merged[key]) && keeperHasMeaningfulValue(value) {
					merged[key] = value
				}
			}
		}
		result[name] = merged
	}
	return result
}

func keeperRemoteAuthFileScore(item map[string]any) int {
	score := 0
	if strings.EqualFold(keeperString(item["source"]), "file") {
		score += 8
	}
	if keeperString(item["path"]) != "" {
		score += 4
	}
	if !keeperBool(item["runtime_only"], item["runtimeOnly"]) {
		score += 2
	}
	if !keeperBool(item["disabled"]) {
		score++
	}
	return score
}

func keeperHasMeaningfulValue(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

func keeperMapHasAnyKey(item map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := item[key]; ok {
			return true
		}
	}
	return false
}

func keeperObject(values ...any) map[string]any {
	for _, value := range values {
		if object, ok := value.(map[string]any); ok {
			return object
		}
	}
	return nil
}

func (a *App) listKeeperAccounts(ctx context.Context) ([]keeperAccount, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT auth_name, email, auth_index, account_type, disabled, priority, primary_used_percent,
		       secondary_used_percent, CAST(primary_reset_at AS TEXT), CAST(secondary_reset_at AS TEXT), quota_threshold,
		       last_status_code, last_error, latest_action, CAST(last_checked_at AS TEXT), CAST(last_healthy_at AS TEXT),
		       primary_window_seconds, secondary_window_seconds, restore_priority, CAST(created_at AS TEXT), CAST(updated_at AS TEXT)
		FROM codex_keeper_auth_states
		ORDER BY COALESCE(email, ''), auth_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []keeperAccount{}
	for rows.Next() {
		state, err := scanKeeperState(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, state.keeperAccount)
	}
	return accounts, rows.Err()
}

func (a *App) syncKeeperAccountList(ctx context.Context) (int, int, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return 0, 0, err
	}
	authFiles, err := a.listKeeperRemoteAuthFiles(ctx, cfg)
	if err != nil {
		return 0, 0, err
	}
	return a.syncKeeperAccountListFromRemote(ctx, authFiles)
}

func (a *App) syncKeeperAccountListFromRemote(ctx context.Context, authFiles []map[string]any) (int, int, error) {
	remoteAccounts := map[string]map[string]any{}
	for name, item := range keeperRemoteAuthFilesByName(authFiles) {
		if !keeperIsCodexProvider(keeperCredentialProvider(item)) {
			continue
		}
		remoteAccounts[name] = item
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	now := dbTime(time.Now().In(appTimeLocation))
	insert, err := tx.PrepareContext(ctx, `
		INSERT INTO codex_keeper_auth_states (
			auth_name, email, auth_index, account_type, disabled, priority, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING
	`)
	if err != nil {
		return 0, 0, err
	}
	defer insert.Close()
	added := 0
	for name, item := range remoteAccounts {
		result, err := insert.ExecContext(
			ctx,
			name,
			keeperStringPtr(item["email"]),
			keeperRemoteAuthIndex(item),
			keeperStringPtr(item["account_type"], item["accountType"]),
			keeperBool(item["disabled"]),
			keeperIntPtr(item["priority"]),
			now,
			now,
		)
		if err != nil {
			return 0, 0, err
		}
		affected, _ := result.RowsAffected()
		added += int(affected)
	}

	rows, err := tx.QueryContext(ctx, `SELECT auth_name FROM codex_keeper_auth_states`)
	if err != nil {
		return 0, 0, err
	}
	stale := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return 0, 0, err
		}
		if _, exists := remoteAccounts[name]; !exists {
			stale = append(stale, name)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	remove, err := tx.PrepareContext(ctx, `DELETE FROM codex_keeper_auth_states WHERE auth_name = ?`)
	if err != nil {
		return 0, 0, err
	}
	defer remove.Close()
	removed := 0
	for _, name := range stale {
		result, err := remove.ExecContext(ctx, name)
		if err != nil {
			return 0, 0, err
		}
		affected, _ := result.RowsAffected()
		removed += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return added, removed, nil
}

func (a *App) pruneKeeperMissingAuthStates(ctx context.Context, remoteNames map[string]bool) (int, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT auth_name FROM codex_keeper_auth_states`)
	if err != nil {
		return 0, err
	}
	stale := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if !remoteNames[name] {
			stale = append(stale, name)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(stale) == 0 {
		return 0, nil
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `DELETE FROM codex_keeper_auth_states WHERE auth_name = ?`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	pruned := 0
	for _, name := range stale {
		result, err := stmt.ExecContext(ctx, name)
		if err != nil {
			return 0, err
		}
		affected, _ := result.RowsAffected()
		pruned += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return pruned, nil
}

func (a *App) getKeeperState(ctx context.Context, name string) (*keeperAuthState, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT auth_name, email, auth_index, account_type, disabled, priority, primary_used_percent,
		       secondary_used_percent, CAST(primary_reset_at AS TEXT), CAST(secondary_reset_at AS TEXT), quota_threshold,
		       last_status_code, last_error, latest_action, CAST(last_checked_at AS TEXT), CAST(last_healthy_at AS TEXT),
		       primary_window_seconds, secondary_window_seconds, restore_priority, CAST(created_at AS TEXT), CAST(updated_at AS TEXT)
		FROM codex_keeper_auth_states WHERE auth_name = ?
	`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, notFoundError("凭证状态不存在")
	}
	state, err := scanKeeperState(rows)
	if err != nil {
		return nil, err
	}
	return &state, rows.Err()
}

func scanKeeperState(scanner interface{ Scan(dest ...any) error }) (keeperAuthState, error) {
	var state keeperAuthState
	var email, authIndex, accountType, primaryReset, secondaryReset, lastError, latestAction, lastChecked, lastHealthy, createdAt, updatedAt sql.NullString
	var priority, primaryUsed, secondaryUsed, quotaThreshold, lastStatus, primaryWindowSeconds, secondaryWindowSeconds, restorePriority sql.NullInt64
	err := scanner.Scan(
		&state.Name, &email, &authIndex, &accountType, &state.Disabled, &priority, &primaryUsed,
		&secondaryUsed, &primaryReset, &secondaryReset, &quotaThreshold, &lastStatus,
		&lastError, &latestAction, &lastChecked, &lastHealthy, &primaryWindowSeconds, &secondaryWindowSeconds, &restorePriority,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return keeperAuthState{}, err
	}
	state.Email = nullableString(email)
	state.AuthIndex = nullableString(authIndex)
	state.AccountType = keeperAccountTypeOrUnknown(nullableString(accountType))
	state.Priority = nullableInt(priority)
	state.PrimaryUsedPercent = nullableInt(primaryUsed)
	state.SecondaryUsedPercent = nullableInt(secondaryUsed)
	state.PrimaryResetAt = timePtr(primaryReset)
	state.SecondaryResetAt = timePtr(secondaryReset)
	state.PrimaryWindowSeconds = nullableInt(primaryWindowSeconds)
	state.SecondaryWindowSeconds = nullableInt(secondaryWindowSeconds)
	state.QuotaThreshold = nullableInt(quotaThreshold)
	state.LastStatusCode = nullableInt(lastStatus)
	state.LastError = nullableString(lastError)
	state.LatestAction = nullableString(latestAction)
	state.LastCheckedAt = timePtr(lastChecked)
	state.LastHealthyAt = timePtr(lastHealthy)
	state.RestorePriority = nullableInt(restorePriority)
	if parsed, ok := parseDBTime(createdAt.String); ok {
		state.CreatedAt = parsed
	}
	if parsed, ok := parseDBTime(updatedAt.String); ok {
		state.UpdatedAt = parsed
	}
	return state, nil
}

func (a *App) upsertKeeperState(ctx context.Context, result keeperAccountResult) error {
	now := dbTime(time.Now())
	checkedAt := dbTime(result.CheckedAt)
	var lastHealthy any
	if result.Result == "healthy" || result.Result == "status_enabled" || result.Result == "priority_degraded" || result.Result == "priority_restored" {
		lastHealthy = checkedAt
	}
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO codex_keeper_auth_states (
			auth_name, email, auth_index, account_type, disabled, priority, restore_priority, latest_action, last_error,
			last_status_code, primary_used_percent, secondary_used_percent, quota_threshold,
			primary_reset_at, secondary_reset_at, primary_window_seconds, secondary_window_seconds,
			last_checked_at, last_healthy_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(auth_name) DO UPDATE SET
			email = excluded.email,
			auth_index = excluded.auth_index,
			account_type = excluded.account_type,
			disabled = excluded.disabled,
			priority = excluded.priority,
			restore_priority = CASE
				WHEN ? <> 0 THEN NULL
				WHEN excluded.restore_priority IS NOT NULL THEN excluded.restore_priority
				ELSE codex_keeper_auth_states.restore_priority
			END,
			latest_action = excluded.latest_action,
			last_error = excluded.last_error,
			last_status_code = excluded.last_status_code,
			primary_used_percent = excluded.primary_used_percent,
			secondary_used_percent = excluded.secondary_used_percent,
			quota_threshold = excluded.quota_threshold,
			primary_reset_at = excluded.primary_reset_at,
			secondary_reset_at = excluded.secondary_reset_at,
			primary_window_seconds = excluded.primary_window_seconds,
			secondary_window_seconds = excluded.secondary_window_seconds,
			last_checked_at = excluded.last_checked_at,
			last_healthy_at = COALESCE(excluded.last_healthy_at, codex_keeper_auth_states.last_healthy_at),
			updated_at = excluded.updated_at
	`, result.Name, result.Email, result.AuthIndex, result.AccountType, boolValue(result.Disabled), result.Priority, result.RestorePriority, result.LatestAction, result.LastError, result.LastStatusCode, result.PrimaryUsedPercent, result.SecondaryUsedPercent, result.QuotaThreshold, dbTimePtr(result.PrimaryResetAt), dbTimePtr(result.SecondaryResetAt), result.PrimaryWindowSeconds, result.SecondaryWindowSeconds, checkedAt, lastHealthy, now, now, result.ClearRestorePriority)
	return err
}

func (a *App) setKeeperAccountDisabled(ctx context.Context, authName string, disabled bool) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	if err := a.setKeeperRemoteDisabled(ctx, cfg, authName, disabled); err != nil {
		return err
	}
	now := dbTime(time.Now())
	var checkedAt any = now
	var lastHealthy any
	if !disabled {
		lastHealthy = now
	}
	_, err = a.db.ExecContext(ctx, `
		UPDATE codex_keeper_auth_states
		SET disabled = ?, restore_priority = NULL, latest_action = NULL, last_error = NULL,
		    last_status_code = NULL, primary_used_percent = CASE WHEN ? <> 0 THEN NULL ELSE primary_used_percent END,
		    secondary_used_percent = CASE WHEN ? <> 0 THEN NULL ELSE secondary_used_percent END,
		    primary_reset_at = CASE WHEN ? <> 0 THEN NULL ELSE primary_reset_at END,
		    secondary_reset_at = CASE WHEN ? <> 0 THEN NULL ELSE secondary_reset_at END,
		    quota_threshold = CASE WHEN ? <> 0 THEN NULL ELSE quota_threshold END,
		    last_checked_at = ?, last_healthy_at = COALESCE(?, last_healthy_at), updated_at = ?
		WHERE auth_name = ?
	`, disabled, disabled, disabled, disabled, disabled, disabled, checkedAt, lastHealthy, now, authName)
	return err
}

func (a *App) deleteKeeperAccount(ctx context.Context, authName string) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	if err := a.deleteKeeperRemoteAuthFile(ctx, cfg, authName); err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `DELETE FROM codex_keeper_auth_states WHERE auth_name = ?`, authName)
	if err == nil {
		a.deleteCachedKeeperResetCredits(authName)
	}
	return err
}

func (a *App) bulkDeleteKeeperAccounts(w http.ResponseWriter, r *http.Request) error {
	var payload keeperBulkDeleteRequest
	if err := decodeJSON(r, &payload); err != nil {
		return err
	}
	names, err := normalizeKeeperAuthNames(payload.AuthNames)
	if err != nil {
		return err
	}
	deleted := []string{}
	failures := []map[string]string{}
	for _, name := range names {
		if err := a.deleteKeeperAccount(r.Context(), name); err != nil {
			failures = append(failures, map[string]string{"name": name, "message": err.Error()})
			continue
		}
		deleted = append(deleted, name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "completed", "deleted": deleted, "failed": failures})
	return nil
}

func (a *App) updateKeeperAccountPriority(ctx context.Context, authName string, priority int) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	authFiles, err := a.listKeeperRemoteAuthFiles(ctx, cfg)
	if err != nil {
		return err
	}
	item, ok := keeperRemoteAuthFilesByName(authFiles)[authName]
	if !ok {
		return notFoundError("凭证不存在")
	}
	if keeperIsCodexProvider(keeperCredentialProvider(item)) {
		accountType := keeperStringPtr(item["account_type"], item["accountType"], item["plan_type"], item["planType"])
		if err := validateKeeperPriority(priority, accountType, cfg.CodexKeeperPriorityRule); err != nil {
			return err
		}
	} else if priority < -1000000 || priority > 1000000 {
		return validationError("凭证优先级必须在 -1000000 到 1000000 之间")
	}
	if err := a.setKeeperRemotePriority(ctx, cfg, authName, &priority); err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `
		UPDATE codex_keeper_auth_states
		SET priority = ?, restore_priority = NULL, latest_action = NULL, last_error = NULL, updated_at = ?
		WHERE auth_name = ?
	`, priority, dbTime(time.Now()), authName)
	return err
}

func (a *App) createKeeperRun(ctx context.Context, mode string) (int, error) {
	now := dbTime(time.Now())
	result, err := a.db.InsertContext(ctx, `
		INSERT INTO codex_keeper_runs (mode, state, detail, started_at, created_at, updated_at)
		VALUES (?, 'running', '', ?, ?, ?)
	`, mode, now, now, now)
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()
	return int(id), nil
}

func (a *App) finishKeeperRun(ctx context.Context, runID int, state, detail string, stats keeperStats) error {
	_, err := a.db.ExecContext(ctx, `
		UPDATE codex_keeper_runs
		SET state = ?, detail = ?, finished_at = ?, total = ?, healthy = ?, status_disabled = ?,
		    status_enabled = ?, priority_degraded = ?, priority_restored = ?, skipped = ?,
		    network_error = ?, updated_at = ?
		WHERE id = ?
	`, state, detail, dbTime(time.Now()), stats.Total, stats.Healthy, stats.StatusDisabled, stats.StatusEnabled, stats.PriorityDegraded, stats.PriorityRestored, stats.Skipped, stats.NetworkError, dbTime(time.Now()), runID)
	return err
}

type keeperRunRecord struct {
	Mode       *string
	State      string
	Detail     string
	StartedAt  *time.Time
	FinishedAt *time.Time
	Stats      keeperStats
}

func (a *App) latestKeeperRun(ctx context.Context) (*keeperRunRecord, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT mode, state, detail, CAST(started_at AS TEXT), CAST(finished_at AS TEXT), total, healthy, status_disabled,
		       status_enabled, priority_degraded, priority_restored, skipped, network_error
		FROM codex_keeper_runs ORDER BY id DESC LIMIT 1
	`)
	return scanKeeperRunRecord(row)
}

func (a *App) latestKeeperRunByMode(ctx context.Context, mode string) (*keeperRunRecord, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT mode, state, detail, CAST(started_at AS TEXT), CAST(finished_at AS TEXT), total, healthy, status_disabled,
		       status_enabled, priority_degraded, priority_restored, skipped, network_error
		FROM codex_keeper_runs WHERE mode = ? ORDER BY id DESC LIMIT 1
	`, mode)
	return scanKeeperRunRecord(row)
}

func scanKeeperRunRecord(row interface {
	Scan(dest ...any) error
}) (*keeperRunRecord, error) {
	var run keeperRunRecord
	var mode, startedAt, finishedAt sql.NullString
	err := row.Scan(&mode, &run.State, &run.Detail, &startedAt, &finishedAt, &run.Stats.Total, &run.Stats.Healthy, &run.Stats.StatusDisabled, &run.Stats.StatusEnabled, &run.Stats.PriorityDegraded, &run.Stats.PriorityRestored, &run.Stats.Skipped, &run.Stats.NetworkError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	run.Mode = nullableString(mode)
	run.StartedAt = timePtr(startedAt)
	run.FinishedAt = timePtr(finishedAt)
	return &run, nil
}

func (a *App) recordKeeperRunAccount(ctx context.Context, runID int, result keeperAccountResult) error {
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO codex_keeper_run_accounts (
			run_id, auth_name, email, result, account_type, priority, disabled,
			keeper_action, primary_used_percent, secondary_used_percent, quota_threshold,
			last_status_code, last_error, latest_action, checked_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, runID, result.Name, result.Email, result.Result, result.AccountType, result.Priority, result.Disabled, valueOr(result.LatestAction, "none"), result.PrimaryUsedPercent, result.SecondaryUsedPercent, result.QuotaThreshold, result.LastStatusCode, result.LastError, result.LatestAction, dbTime(result.CheckedAt), dbTime(time.Now()))
	return err
}

func extractKeeperObjects(payload any, keys []string) []map[string]any {
	if items, ok := payload.([]any); ok {
		return mapItems(items)
	}
	object, ok := payload.(map[string]any)
	if !ok {
		return []map[string]any{}
	}
	for _, key := range keys {
		if items, ok := object[key].([]any); ok {
			return mapItems(items)
		}
	}
	return []map[string]any{}
}

func mapItems(items []any) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			result = append(result, object)
		}
	}
	return result
}

func mergeKeeperObjects(left, right map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range left {
		result[key] = value
	}
	for key, value := range right {
		result[key] = value
	}
	return result
}

func parseKeeperUsageInfo(payload map[string]any) keeperUsageInfo {
	usage := keeperUsageInfo{PlanType: "unknown"}
	if payload == nil {
		return usage
	}
	if value := keeperString(payload["plan_type"]); value != "" {
		usage.PlanType = value
	} else if value := keeperString(payload["planType"]); value != "" {
		usage.PlanType = value
	}
	rateLimit, _ := payload["rate_limit"].(map[string]any)
	primary, _ := rateLimit["primary_window"].(map[string]any)
	secondary, _ := rateLimit["secondary_window"].(map[string]any)
	if value := keeperIntPtr(primary["used_percent"]); value != nil {
		usage.PrimaryUsedPercent = *value
	}
	usage.SecondaryUsedPercent = keeperIntPtr(secondary["used_percent"])
	usage.PrimaryResetAt = quotaResetAt(primary, time.Now().In(appTimeLocation))
	usage.SecondaryResetAt = quotaResetAt(secondary, time.Now().In(appTimeLocation))
	usage.PrimaryWindowSeconds = quotaWindowSeconds(primary)
	usage.SecondaryWindowSeconds = quotaWindowSeconds(secondary)
	return usage
}

func quotaWindowSeconds(window map[string]any) *int {
	if window == nil {
		return nil
	}
	value := keeperIntPtr(
		window["limit_window_seconds"],
		window["limitWindowSeconds"],
		window["window_seconds"],
		window["windowSeconds"],
		window["rolling_window_seconds"],
		window["rollingWindowSeconds"],
	)
	if value == nil || *value <= 0 {
		return nil
	}
	return value
}

func quotaResetAt(window map[string]any, base time.Time) *time.Time {
	if window == nil {
		return nil
	}
	if ts := keeperIntPtr(window["reset_at"], window["resetAt"], window["reset_at_seconds"], window["resetAtSeconds"]); ts != nil {
		seconds := int64(*ts)
		if seconds > 10_000_000_000 {
			seconds /= 1000
		}
		parsed := time.Unix(seconds, 0).In(appTimeLocation)
		return &parsed
	}
	if after := keeperIntPtr(window["reset_after_seconds"], window["resetAfterSeconds"]); after != nil && *after >= 0 {
		parsed := base.Add(time.Duration(*after) * time.Second)
		return &parsed
	}
	return nil
}

func accountTypeFromKeeperDetail(detail map[string]any, usage *keeperUsageInfo) *string {
	values := []string{}
	if usage != nil {
		values = append(values, usage.PlanType)
	}
	values = append(values, keeperAccountTypeValues(detail)...)
	text := strings.ToLower(strings.Join(values, " "))
	text = strings.NewReplacer("-", "_", " ", "_", ".", "_", "@", "_", "/", "_", "\\", "_").Replace(text)
	bounded := "_" + text + "_"
	var result string
	switch {
	case strings.Contains(bounded, "_k12_"):
		result = "k12"
	case strings.Contains(text, "prolite") || strings.Contains(text, "pro_lite") || strings.Contains(text, "5x") || strings.Contains(text, "pro_5"):
		result = "pro_5x"
	case strings.Contains(text, "20x") || strings.Contains(text, "pro_20") || strings.Contains(bounded, "_pro_"):
		result = "pro_20x"
	case strings.Contains(text, "team") || strings.Contains(text, "business"):
		result = "team"
	case strings.Contains(text, "plus"):
		result = "plus"
	case strings.Contains(text, "free"):
		result = "free"
	default:
		result = "unknown"
	}
	return &result
}

func keeperAccountTypeOrUnknown(accountType *string) *string {
	if accountType == nil || strings.TrimSpace(*accountType) == "" {
		result := "unknown"
		return &result
	}
	result := strings.ToLower(strings.TrimSpace(*accountType))
	return &result
}

func keeperAccountTypeValues(detail map[string]any) []string {
	if detail == nil {
		return nil
	}
	values := []string{}
	appendString := func(value any) {
		if text := keeperString(value); text != "" {
			values = append(values, text)
		}
	}
	for _, key := range []string{"plan_type", "planType", "plan", "tier", "account_plan", "subscription_plan", "sku", "account_type", "accountType"} {
		appendString(detail[key])
	}
	for _, key := range []string{"attributes", "metadata"} {
		if object, ok := detail[key].(map[string]any); ok {
			for _, nestedKey := range []string{"plan_type", "planType", "chatgpt_plan_type", "chatgptPlanType"} {
				appendString(object[nestedKey])
			}
			values = append(values, keeperIDTokenPlanValues(object["id_token"])...)
		}
	}
	values = append(values, keeperIDTokenPlanValues(detail["id_token"])...)
	for _, key := range []string{"name", "file_name", "filename"} {
		appendString(detail[key])
	}
	if path := keeperString(detail["path"]); path != "" {
		values = append(values, filepath.Base(path))
	}
	return values
}

func keeperIDTokenPlanValues(value any) []string {
	object := keeperIDTokenClaims(value)
	if object == nil {
		return nil
	}
	values := []string{}
	appendString := func(value any) {
		if text := keeperString(value); text != "" {
			values = append(values, text)
		}
	}
	for _, key := range []string{"plan_type", "planType", "chatgpt_plan_type", "chatgptPlanType"} {
		appendString(object[key])
	}
	if authInfo, ok := object["https://api.openai.com/auth"].(map[string]any); ok {
		for _, key := range []string{"plan_type", "planType", "chatgpt_plan_type", "chatgptPlanType"} {
			appendString(authInfo[key])
		}
	}
	return values
}

func keeperIDTokenClaims(value any) map[string]any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return typed
	case string:
		token := strings.TrimSpace(typed)
		if token == "" {
			return nil
		}
		var object map[string]any
		if json.Unmarshal([]byte(token), &object) == nil {
			return object
		}
		parts := strings.Split(token, ".")
		if len(parts) < 2 {
			return nil
		}
		decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(parts[1])
		}
		if err != nil {
			return nil
		}
		if json.Unmarshal(decoded, &object) != nil {
			return nil
		}
		return object
	default:
		return nil
	}
}

func keeperPriorityForType(accountType *string, rules map[string]int) *int {
	key := "unknown"
	if accountType != nil {
		if normalized := strings.ToLower(strings.TrimSpace(*accountType)); normalized != "" {
			key = normalized
		}
	}
	value, ok := normalizePriorityRules(rules)[key]
	if !ok {
		return nil
	}
	return &value
}

func validateKeeperPriority(priority int, accountType *string, rules map[string]int) error {
	if priority < -1 || priority > 20 {
		return nil
	}
	expected := keeperPriorityForType(accountType, rules)
	if expected != nil && *expected == priority {
		return nil
	}
	if accountType == nil || expected == nil {
		return validationError("该凭证类型没有可设置的系统 priority")
	}
	return validationError(fmt.Sprintf("只能设置小于 -1、大于 20，或当前凭证类型 %s 对应的 priority %d", *accountType, *expected))
}

func isBadKeeperCredential(result keeperHTTPResult) bool {
	if isKeeperTransientWarning(result) {
		return false
	}
	if result.StatusCode != nil && (*result.StatusCode == 401 || *result.StatusCode == 402) {
		return true
	}
	text := strings.ToLower(result.Brief)
	if result.JSONData != nil {
		payload, _ := json.Marshal(result.JSONData)
		text += " " + strings.ToLower(string(payload))
	}
	return strings.Contains(text, "workspace") && (strings.Contains(text, "disabled") || strings.Contains(text, "deactivated"))
}

func isKeeperTransientWarning(result keeperHTTPResult) bool {
	text := strings.ToLower(result.Brief)
	if result.JSONData != nil {
		payload, _ := json.Marshal(result.JSONData)
		text += " " + strings.ToLower(string(payload))
	}
	return strings.Contains(text, "service_unavailable_error") || strings.Contains(text, "server_is_overloaded")
}

func (a *App) preserveKeeperBadCredentialDiagnosis(ctx context.Context, result *keeperAccountResult) {
	state, err := a.getKeeperState(ctx, result.Name)
	if err != nil || !isKeeperBadCredentialDisableAction(state.LatestAction) {
		return
	}
	result.LastStatusCode = state.LastStatusCode
	result.LastError = cloneStringPtr(state.LastError)
	result.LatestAction = cloneStringPtr(state.LatestAction)
}

func isKeeperRecoverableUnauthorizedDisabledState(state *keeperAuthState) bool {
	return state != nil &&
		state.Disabled &&
		state.LastStatusCode != nil &&
		*state.LastStatusCode == http.StatusUnauthorized &&
		isKeeperBadCredentialDisableAction(state.LatestAction)
}

func isKeeperBadCredentialDisableAction(action *string) bool {
	if action == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(*action), "禁用凭证")
}

func keeperBodyJSON(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	text, ok := value.(string)
	if !ok {
		return nil
	}
	var object map[string]any
	if json.Unmarshal([]byte(text), &object) != nil {
		return nil
	}
	return object
}

func keeperAuthIndex(detail map[string]any) string {
	for _, key := range []string{"auth_index", "authIndex", "index", "name"} {
		if value := keeperString(detail[key]); value != "" {
			return value
		}
	}
	return "unknown"
}

func keeperRemoteAuthIndex(detail map[string]any) *string {
	for _, key := range []string{"auth_index", "authIndex", "index"} {
		if value := keeperString(detail[key]); value != "" {
			return &value
		}
	}
	return nil
}

func keeperString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func keeperStringPtr(values ...any) *string {
	for _, value := range values {
		if text := keeperString(value); text != "" {
			return &text
		}
	}
	return nil
}

func keeperIntPtr(values ...any) *int {
	for _, value := range values {
		switch typed := value.(type) {
		case nil:
			continue
		case int:
			return &typed
		case int64:
			converted := int(typed)
			return &converted
		case float64:
			converted := int(typed)
			return &converted
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(typed))
			if err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func keeperBool(values ...any) bool {
	for _, value := range values {
		switch typed := value.(type) {
		case bool:
			if typed {
				return true
			}
		case string:
			normalized := strings.ToLower(strings.TrimSpace(typed))
			if normalized == "1" || normalized == "true" || normalized == "yes" || normalized == "on" {
				return true
			}
		case float64:
			if typed != 0 {
				return true
			}
		}
	}
	return false
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func briefPayload(payload []byte) string {
	text := strings.TrimSpace(string(payload))
	if len(text) > 160 {
		return text[:160] + "..."
	}
	return text
}

func briefAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		if len(typed) > 160 {
			return typed[:160] + "..."
		}
		return typed
	default:
		payload, _ := json.Marshal(typed)
		return briefPayload(payload)
	}
}

func normalizeKeeperAuthNames(raw []string) ([]string, error) {
	result, err := normalizeOptionalKeeperAuthNames(raw)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, validationError("凭证名称不能为空")
	}
	return result, nil
}

func normalizeOptionalKeeperAuthNames(raw []string) ([]string, error) {
	seen := map[string]bool{}
	result := []string{}
	for _, item := range raw {
		name := strings.TrimSpace(item)
		if name == "" {
			return nil, validationError("凭证名称不能为空")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result, nil
}

func waitForStop(stop <-chan struct{}, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-stop:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-stop:
		return true
	case <-timer.C:
		return false
	}
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func sortKeeperAccounts(accounts []keeperAccount) {
	sort.Slice(accounts, func(i, j int) bool {
		left := valueOr(accounts[i].Email, "") + accounts[i].Name
		right := valueOr(accounts[j].Email, "") + accounts[j].Name
		return left < right
	})
}
