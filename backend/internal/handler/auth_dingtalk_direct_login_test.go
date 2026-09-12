//go:build unit

package handler

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/ent/pendingauthsession"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

const dingTalkDirectLoginTestUnionID = "union_direct_login"

// newDingTalkDirectLoginTestHandler 构造带 sqlite 内存库的 AuthHandler，
// 复用 wechat OAuth 测试的 setting repo / refreshToken cache stub。
func newDingTalkDirectLoginTestHandler(t *testing.T, invitationEnabled bool) (*AuthHandler, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:auth_dingtalk_direct?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))

	userRepo := &oauthPendingFlowUserRepo{client: client}
	redeemRepo := repository.NewRedeemCodeRepository(client)
	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:                   "test-secret",
			ExpireHour:               1,
			AccessTokenExpireMinutes: 60,
			RefreshTokenExpireDays:   7,
		},
		Default: config.DefaultConfig{UserBalance: 0, UserConcurrency: 1},
	}
	values := map[string]string{
		service.SettingKeyRegistrationEnabled:          "true",
		service.SettingKeyInvitationCodeEnabled:        boolSettingValue(invitationEnabled),
		service.SettingKeyDingTalkConnectEnabled:       "true",
		service.SettingKeyDingTalkConnectClientID:      "dt-client",
		service.SettingKeyDingTalkConnectClientSecret:  "dt-secret",
		service.SettingKeyDingTalkConnectRedirectURL:   "https://api.example.com/api/v1/auth/oauth/dingtalk/callback",
		service.SettingKeyDingTalkConnectAutoProvision: "true",
	}
	settingSvc := service.NewSettingService(&wechatOAuthSettingRepoStub{values: values}, cfg)

	authSvc := service.NewAuthService(
		client,
		userRepo,
		redeemRepo,
		&wechatOAuthRefreshTokenCacheStub{},
		cfg,
		settingSvc,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	return &AuthHandler{authService: authSvc, settingSvc: settingSvc, cfg: cfg}, client
}

func dingTalkDirectLoginContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c
}

func dingTalkDirectLoginIdentityKey() service.PendingAuthIdentityKey {
	return service.PendingAuthIdentityKey{
		ProviderType:    "dingtalk",
		ProviderKey:     "dingtalk",
		ProviderSubject: dingTalkDirectLoginTestUnionID,
	}
}

// TestStartDingTalkDirectLogin_ProvisionsUserAndPendingSession 是回归测试。
//
// 背景：钉钉回调建号若只创建 pending session 而不写 TargetUserID，
// pendingOAuthCompletionCanIssueTokenPair 会返回 false → exchange 不签发 token
// → 前端 getOAuthCompletionKind 把它误判为 'bind'，弹「绑定成功」但实际未登录。
// 这正是「钉钉扫码后不会自动创建账号」的根因，此测试用于锁死该行为。
func TestStartDingTalkDirectLogin_ProvisionsUserAndPendingSession(t *testing.T) {
	handler, client := newDingTalkDirectLoginTestHandler(t, false)
	c := dingTalkDirectLoginContext(t)

	handled, err := handler.startDingTalkDirectLogin(
		c,
		config.DingTalkConnectConfig{Enabled: true, AutoProvision: true},
		dingTalkDirectLoginIdentityKey(),
		"a001@fjdaze.com",
		"张三",
		"a001",
		&DingTalkStaffInfo{UserID: "u1", Name: "张三", JobNumber: "A001"},
		"/dashboard",
		"browser-key",
		map[string]any{"username": "张三", "job_number": "A001"},
	)
	require.NoError(t, err)
	require.True(t, handled, "非邀请码模式下应建号成功")

	ctx := context.Background()
	created, err := client.User.Query().Where(dbuser.EmailEQ("a001@fjdaze.com")).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "张三", created.Username)

	// 初始密码必须是邮箱 @ 前部分
	require.True(t, handler.authService.CheckPassword("a001", created.PasswordHash),
		"初始密码应为邮箱 @ 前部分")
	require.False(t, handler.authService.CheckPassword("wrong-password", created.PasswordHash))

	// 关键断言：pending session 必须带 TargetUserID，否则 exchange 不签发 token
	session, err := client.PendingAuthSession.Query().
		Where(pendingauthsession.ProviderSubjectEQ(dingTalkDirectLoginTestUnionID)).
		Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, session.TargetUserID,
		"pending session 必须带 TargetUserID，否则 exchange 不会签发 token")
	require.Equal(t, created.ID, *session.TargetUserID)
	require.Equal(t, oauthIntentLogin, session.Intent)
}

// TestStartDingTalkDirectLogin_InvitationModeDoesNotProvision 邀请码模式下必须回退：
// 既不建号也不建 session，交回上层渲染邀请码输入框（而非报错页或静默"成功"）。
func TestStartDingTalkDirectLogin_InvitationModeDoesNotProvision(t *testing.T) {
	handler, client := newDingTalkDirectLoginTestHandler(t, true)
	c := dingTalkDirectLoginContext(t)

	handled, err := handler.startDingTalkDirectLogin(
		c,
		config.DingTalkConnectConfig{Enabled: true, AutoProvision: true},
		dingTalkDirectLoginIdentityKey(),
		"a002@fjdaze.com",
		"李四",
		"a002",
		&DingTalkStaffInfo{UserID: "u2", Name: "李四", JobNumber: "A002"},
		"/dashboard",
		"browser-key",
		map[string]any{"username": "李四"},
	)
	require.NoError(t, err)
	require.False(t, handled, "邀请码模式下应回退而非报错")

	ctx := context.Background()
	userCount, err := client.User.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, userCount, "邀请码模式下不应建号")

	sessionCount, err := client.PendingAuthSession.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, sessionCount, "回退时不应创建 pending session")
}
