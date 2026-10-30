package api_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"testing"
	"time"

	"household-bills/backend/internal/store"
)

type avatarResponse struct {
	status int
	body   []byte
	header http.Header
}

func (r avatarResponse) expect(t *testing.T, want int) avatarResponse {
	t.Helper()
	if r.status != want {
		t.Fatalf("头像请求状态码 = %d，期望 %d；响应: %s", r.status, want, string(r.body))
	}
	return r
}

// uploadUserAvatar sends the raw image bytes without going through the JSON
// encoder. The request still uses the client's real cookie jar, Origin and CSRF
// token exactly like the frontend does.
func uploadUserAvatar(
	t *testing.T,
	client *testClient,
	userID int64,
	contentType string,
	data []byte,
	mutations ...func(*http.Request),
) testResponse {
	t.Helper()
	request, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("%s/api/users/%d/avatar/", client.base, userID),
		bytes.NewReader(data))
	if err != nil {
		t.Fatalf("创建头像上传请求失败: %v", err)
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Origin", client.base)
	if token := client.cookieValue("csrftoken"); token != "" {
		request.Header.Set("X-CSRFToken", token)
	}
	for _, mutate := range mutations {
		mutate(request)
	}

	response, err := client.client.Do(request)
	if err != nil {
		t.Fatalf("上传头像失败: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("读取头像上传响应失败: %v", err)
	}
	return testResponse{status: response.StatusCode, body: body}
}

func fetchUserAvatar(t *testing.T, client *testClient, userID int64, version int) avatarResponse {
	t.Helper()
	requestPath := fmt.Sprintf("%s/api/users/%d/avatar/", client.base, userID)
	if version >= 0 {
		requestPath += fmt.Sprintf("?v=%d", version)
	}
	request, err := http.NewRequest(http.MethodGet, requestPath, nil)
	if err != nil {
		t.Fatalf("创建头像读取请求失败: %v", err)
	}
	response, err := client.client.Do(request)
	if err != nil {
		t.Fatalf("读取头像失败: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("读取头像响应失败: %v", err)
	}
	return avatarResponse{
		status: response.StatusCode,
		body:   body,
		header: response.Header.Clone(),
	}
}

func expectAvatarFieldError(t *testing.T, response testResponse) {
	t.Helper()
	if response.status != http.StatusBadRequest {
		t.Fatalf("非法头像状态码 = %d，期望 400；响应: %s", response.status, string(response.body))
	}
	if _, exists := response.object(t)["avatar"]; !exists {
		t.Fatalf("非法头像应返回 avatar 字段错误: %s", string(response.body))
	}
}

func avatarTestImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(17 + (x*31+y*7)%220),
				G: uint8(29 + (x*11+y*43)%210),
				B: uint8(41 + (x*19+y*13)%200),
				A: 255,
			})
		}
	}
	return img
}

func avatarPNG(t *testing.T) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, avatarTestImage(4, 3)); err != nil {
		t.Fatalf("编码 PNG 失败: %v", err)
	}
	return encoded.Bytes()
}

func avatarJPEG(t *testing.T) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, avatarTestImage(5, 4), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("编码 JPEG 失败: %v", err)
	}
	return encoded.Bytes()
}

// oversizedAvatarPNG is a decodable 1024x1024 image whose incompressible pixel
// data makes the encoded file exceed the 2 MiB byte limit.
func oversizedAvatarPNG(t *testing.T) []byte {
	t.Helper()
	const width, height = 1024, 1024
	pixels := make([]byte, width*height*4)
	random := rand.New(rand.NewSource(20261030))
	if _, err := random.Read(pixels); err != nil {
		t.Fatalf("生成随机头像像素失败: %v", err)
	}
	img := &image.NRGBA{
		Pix:    pixels,
		Stride: width * 4,
		Rect:   image.Rect(0, 0, width, height),
	}
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&encoded, img); err != nil {
		t.Fatalf("编码超大 PNG 失败: %v", err)
	}
	if encoded.Len() <= 2<<20 {
		t.Fatalf("超大 PNG 仅 %d 字节，未超过 2MiB 上限", encoded.Len())
	}
	return encoded.Bytes()
}

// oversizedDimensionPNG is a tiny valid file whose decoded width exceeds the
// per-side limit of 4096.
func oversizedDimensionPNG(t *testing.T) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, avatarTestImage(4097, 1)); err != nil {
		t.Fatalf("编码超宽 PNG 失败: %v", err)
	}
	return encoded.Bytes()
}

func findMember(t *testing.T, me map[string]any, username string) map[string]any {
	t.Helper()
	for _, item := range mustSlice(t, me["members"]) {
		member := mustMap(t, item)
		if mustString(t, member["username"]) == username {
			return member
		}
	}
	t.Fatalf("家庭成员 %q 不存在: %s", username, mustJSON(t, me))
	return nil
}

func memberUsernames(t *testing.T, me map[string]any) map[string]bool {
	t.Helper()
	names := make(map[string]bool)
	for _, item := range mustSlice(t, me["members"]) {
		names[mustString(t, mustMap(t, item)["username"])] = true
	}
	return names
}

func findListedUser(t *testing.T, users []map[string]any, username string) map[string]any {
	t.Helper()
	for _, user := range users {
		if mustString(t, user["username"]) == username {
			return user
		}
	}
	t.Fatalf("用户列表缺少 %q: %s", username, mustJSON(t, users))
	return nil
}

func listedUserExists(t *testing.T, users []map[string]any, username string) bool {
	t.Helper()
	for _, user := range users {
		if mustString(t, user["username"]) == username {
			return true
		}
	}
	return false
}

func TestUserProfileUpdateAndLogin(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)

	adminMe := admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	adminID := jsonInt64(t, mustMap(t, adminMe["user"])["id"])

	aliceCreated := admin.object(t, http.MethodPost, "/api/users/", map[string]any{
		"username": "alice", "nickname": "Alice", "password": strongPassword,
	}, http.StatusCreated)
	aliceID := jsonInt64(t, aliceCreated["id"])
	if mustString(t, aliceCreated["nickname"]) != "Alice" {
		t.Fatalf("创建用户昵称错误: %s", mustJSON(t, aliceCreated))
	}
	if got := jsonInt64(t, aliceCreated["avatar_version"]); got != 0 {
		t.Fatalf("新用户头像版本 = %d，期望 0", got)
	}

	alice := env.loginUser(t, "alice", strongPassword)
	aliceSession := alice.cookieValue("sessionid")

	updated := alice.object(t, http.MethodPatch, fmt.Sprintf("/api/users/%d/", aliceID),
		map[string]any{"username": "alice-renamed", "nickname": "艾丽丝"}, http.StatusOK)
	if jsonInt64(t, updated["id"]) != aliceID ||
		mustString(t, updated["username"]) != "alice-renamed" ||
		mustString(t, updated["nickname"]) != "艾丽丝" ||
		mustBool(t, updated["is_admin"]) ||
		jsonInt64(t, updated["avatar_version"]) != 0 {
		t.Fatalf("本人资料修改响应错误: %s", mustJSON(t, updated))
	}
	if alice.cookieValue("sessionid") != aliceSession {
		t.Fatal("修改个人资料不应替换会话")
	}

	me := alice.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	user := mustMap(t, me["user"])
	if mustString(t, user["username"]) != "alice-renamed" ||
		mustString(t, user["nickname"]) != "艾丽丝" {
		t.Fatalf("本人 me 未反映资料修改: %s", mustJSON(t, me))
	}
	if member := findMember(t, me, "alice-renamed"); mustString(t, member["nickname"]) != "艾丽丝" {
		t.Fatalf("家庭成员列表未反映昵称: %s", mustJSON(t, me))
	}

	oldLogin := newTestClient(t, env)
	oldResponse := oldLogin.do(http.MethodPost, "/api/auth/login/", map[string]any{
		"username": "alice", "password": strongPassword,
	}).expect(t, http.StatusBadRequest)
	if detail := mustString(t, oldResponse.object(t)["detail"]); detail != "用户名或密码错误。" {
		t.Fatalf("旧用户名登录 detail = %q", detail)
	}
	renamed := env.loginUser(t, "alice-renamed", strongPassword)
	renamedMe := renamed.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if mustString(t, mustMap(t, renamedMe["user"])["nickname"]) != "艾丽丝" {
		t.Fatalf("新用户名登录后昵称错误: %s", mustJSON(t, renamedMe))
	}

	adminUpdated := admin.object(t, http.MethodPatch, fmt.Sprintf("/api/users/%d/", aliceID),
		map[string]any{"username": "alice-final", "nickname": "管理员改名"}, http.StatusOK)
	if mustString(t, adminUpdated["username"]) != "alice-final" ||
		mustString(t, adminUpdated["nickname"]) != "管理员改名" {
		t.Fatalf("管理员修改成员资料响应错误: %s", mustJSON(t, adminUpdated))
	}
	me = alice.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	user = mustMap(t, me["user"])
	if mustString(t, user["username"]) != "alice-final" ||
		mustString(t, user["nickname"]) != "管理员改名" {
		t.Fatalf("管理员修改后原会话应读取新资料: %s", mustJSON(t, me))
	}
	users := admin.array(t, http.MethodGet, "/api/users/", nil, http.StatusOK)
	if listed := findListedUser(t, users, "alice-final"); mustString(t, listed["nickname"]) != "管理员改名" {
		t.Fatalf("用户列表昵称错误: %s", mustJSON(t, users))
	}

	// The administrator login name is fixed so audit history cannot silently
	// point at a different identity.
	admin.do(http.MethodPatch, fmt.Sprintf("/api/users/%d/", adminID),
		map[string]any{"username": "root"}).expect(t, http.StatusBadRequest)
	adminMe = admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if mustString(t, mustMap(t, adminMe["user"])["username"]) != adminUsername {
		t.Fatalf("管理员用户名不应被修改: %s", mustJSON(t, adminMe))
	}

	// Another ordinary member cannot edit someone else in the same household.
	bob := admin.createUser(t, "bob", strongPassword)
	bobID := jsonInt64(t, bob["id"])
	alice.do(http.MethodPatch, fmt.Sprintf("/api/users/%d/", bobID),
		map[string]any{"nickname": "越权修改"}).expect(t, http.StatusForbidden)
	bobClient := env.loginUser(t, "bob", strongPassword)
	bobMe := bobClient.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if nickname := mustString(t, mustMap(t, bobMe["user"])["nickname"]); nickname != "" {
		t.Fatalf("普通成员越权修改后昵称 = %q，期望空", nickname)
	}

	// Cross-household targets are hidden as 404 for both ordinary members and
	// administrators; they must never be reported as editable.
	_, outsiderID := env.seedUser(t, "另一个家庭", "outsider", strongPassword, false)
	outsider := env.loginUser(t, "outsider", strongPassword)
	outsider.do(http.MethodPatch, fmt.Sprintf("/api/users/%d/", aliceID),
		map[string]any{"nickname": "跨家庭修改"}).expect(t, http.StatusNotFound)
	admin.do(http.MethodPatch, fmt.Sprintf("/api/users/%d/", outsiderID),
		map[string]any{"nickname": "跨家庭修改"}).expect(t, http.StatusNotFound)
	outsiderMe := outsider.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if nickname := mustString(t, mustMap(t, outsiderMe["user"])["nickname"]); nickname != "" {
		t.Fatalf("跨家庭修改不应生效: %s", mustJSON(t, outsiderMe))
	}

	adminMe = admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if jsonInt64(t, mustMap(t, adminMe["user"])["id"]) != adminID ||
		mustString(t, mustMap(t, adminMe["user"])["username"]) != adminUsername {
		t.Fatalf("资料修改期间管理员身份发生变化: %s", mustJSON(t, adminMe))
	}
}

func TestUserProfilePasswordRotation(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	adminMe := admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	adminID := jsonInt64(t, mustMap(t, adminMe["user"])["id"])
	adminSession := admin.cookieValue("sessionid")

	// The administrator changing their own password is still a self-service
	// change, so the current password is mandatory.
	admin.do(http.MethodPatch, fmt.Sprintf("/api/users/%d/", adminID),
		map[string]any{"password": rotatedPassword}).expect(t, http.StatusBadRequest)
	if admin.cookieValue("sessionid") != adminSession {
		t.Fatal("被拒绝的管理员改密不应影响现有会话")
	}
	adminMe = admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if !mustBool(t, mustMap(t, adminMe["user"])["is_admin"]) {
		t.Fatalf("被拒绝的管理员改密不应影响管理员身份: %s", mustJSON(t, adminMe))
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", adminID); got != 1 {
		t.Fatalf("管理员会话数量 = %d，期望 1", got)
	}

	carol := admin.createUser(t, "carol", strongPassword)
	carolID := jsonInt64(t, carol["id"])
	carolOne := env.loginUser(t, "carol", strongPassword)
	carolOneToken := carolOne.cookieValue("sessionid")
	carolTwo := env.loginUser(t, "carol", strongPassword)
	carolTwoToken := carolTwo.cookieValue("sessionid")
	if carolOneToken == "" || carolTwoToken == "" || carolOneToken == carolTwoToken {
		t.Fatal("两次登录应产生不同的有效会话")
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", carolID); got != 2 {
		t.Fatalf("改密前会话数量 = %d，期望 2", got)
	}

	// A wrong current password changes nothing: existing sessions stay valid
	// and neither the old nor the proposed password is replaced.
	wrongChange := carolOne.do(http.MethodPatch, fmt.Sprintf("/api/users/%d/", carolID),
		map[string]any{
			"password":         rotatedPassword,
			"current_password": "definitely not the current password",
		}).expect(t, http.StatusBadRequest)
	if _, exists := wrongChange.object(t)["current_password"]; !exists {
		t.Fatalf("当前密码错误应返回 current_password 字段错误: %s", string(wrongChange.body))
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", carolID); got != 2 {
		t.Fatalf("当前密码错误后会话数量 = %d，期望 2", got)
	}
	carolMe := carolTwo.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if mustString(t, mustMap(t, carolMe["user"])["username"]) != "carol" {
		t.Fatalf("当前密码错误不应影响已有会话: %s", mustJSON(t, carolMe))
	}
	newTestClient(t, env).do(http.MethodPost, "/api/auth/login/", map[string]any{
		"username": "carol", "password": rotatedPassword,
	}).expect(t, http.StatusBadRequest)
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", carolID); got != 2 {
		t.Fatalf("失败的改密尝试不应创建会话，实际 %d", got)
	}
	env.loginUser(t, "carol", strongPassword)

	// A correct self-service rotation revokes every session for that user,
	// including the one that performed the change.
	rotated := carolOne.object(t, http.MethodPatch, fmt.Sprintf("/api/users/%d/", carolID),
		map[string]any{
			"password":         rotatedPassword,
			"current_password": strongPassword,
		}, http.StatusOK)
	if mustString(t, rotated["username"]) != "carol" ||
		jsonInt64(t, rotated["avatar_version"]) != 0 {
		t.Fatalf("改密响应用户信息错误: %s", mustJSON(t, rotated))
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", carolID); got != 0 {
		t.Fatalf("改密后会话数量 = %d，期望 0", got)
	}
	for _, token := range []string{carolOneToken, carolTwoToken} {
		replayed := replaySession(t, carolOne.base, token)
		replayed.expect(t, http.StatusOK)
		if replayed.object(t)["user"] != nil {
			t.Fatalf("改密后旧会话 %q 仍然可用", token)
		}
	}
	carolOne.do(http.MethodGet, "/api/accounts/", nil).expect(t, http.StatusUnauthorized)
	oldLogin := newTestClient(t, env)
	oldLogin.do(http.MethodPost, "/api/auth/login/", map[string]any{
		"username": "carol", "password": strongPassword,
	}).expect(t, http.StatusBadRequest)
	newCarol := env.loginUser(t, "carol", rotatedPassword)
	newCarolMe := newCarol.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if mustString(t, mustMap(t, newCarolMe["user"])["username"]) != "carol" {
		t.Fatalf("新密码登录失败: %s", mustJSON(t, newCarolMe))
	}

	// The administrator was not affected by another member's rotation.
	if admin.cookieValue("sessionid") != adminSession {
		t.Fatal("成员改密不应替换管理员会话")
	}
	adminMe = admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if !mustBool(t, mustMap(t, adminMe["user"])["is_admin"]) {
		t.Fatalf("成员改密后管理员会话失效: %s", mustJSON(t, adminMe))
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", adminID); got != 1 {
		t.Fatalf("成员改密后管理员会话数量 = %d，期望 1", got)
	}

	// An administrator may reset an ordinary member's password without knowing
	// the member's current password, and the administrator stays logged in.
	dave := admin.createUser(t, "dave", strongPassword)
	daveID := jsonInt64(t, dave["id"])
	daveClient := env.loginUser(t, "dave", strongPassword)
	daveToken := daveClient.cookieValue("sessionid")
	davePassword := "administrator reset phrase"
	reset := admin.object(t, http.MethodPatch, fmt.Sprintf("/api/users/%d/", daveID),
		map[string]any{"password": davePassword}, http.StatusOK)
	if mustString(t, reset["username"]) != "dave" {
		t.Fatalf("管理员重置密码响应用户错误: %s", mustJSON(t, reset))
	}
	if admin.cookieValue("sessionid") != adminSession {
		t.Fatal("管理员重置成员密码不应替换管理员会话")
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", daveID); got != 0 {
		t.Fatalf("管理员重置后成员会话数量 = %d，期望 0", got)
	}
	replayedDave := replaySession(t, daveClient.base, daveToken)
	replayedDave.expect(t, http.StatusOK)
	if replayedDave.object(t)["user"] != nil {
		t.Fatal("管理员重置密码后成员旧会话仍然可用")
	}
	daveClient.do(http.MethodGet, "/api/accounts/", nil).expect(t, http.StatusUnauthorized)
	newTestClient(t, env).do(http.MethodPost, "/api/auth/login/", map[string]any{
		"username": "dave", "password": strongPassword,
	}).expect(t, http.StatusBadRequest)
	env.loginUser(t, "dave", davePassword)
	adminMe = admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if jsonInt64(t, mustMap(t, adminMe["user"])["id"]) != adminID {
		t.Fatalf("管理员重置成员密码后自身会话失效: %s", mustJSON(t, adminMe))
	}
}

func TestUserAvatarLifecycle(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)

	memberCreated := admin.createUser(t, "avatar-member", strongPassword)
	memberID := jsonInt64(t, memberCreated["id"])
	if got := jsonInt64(t, memberCreated["avatar_version"]); got != 0 {
		t.Fatalf("新成员头像版本 = %d，期望 0", got)
	}
	member := env.loginUser(t, "avatar-member", strongPassword)
	me := member.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if got := jsonInt64(t, mustMap(t, me["user"])["avatar_version"]); got != 0 {
		t.Fatalf("me 中新成员头像版本 = %d，期望 0", got)
	}
	if got := jsonInt64(t, findMember(t, me, "avatar-member")["avatar_version"]); got != 0 {
		t.Fatalf("家庭成员中新成员头像版本 = %d，期望 0", got)
	}
	users := admin.array(t, http.MethodGet, "/api/users/", nil, http.StatusOK)
	if got := jsonInt64(t, findListedUser(t, users, "avatar-member")["avatar_version"]); got != 0 {
		t.Fatalf("用户列表中新成员头像版本 = %d，期望 0", got)
	}

	pngBytes := avatarPNG(t)
	uploaded := uploadUserAvatar(t, member, memberID, "image/png", pngBytes).
		expect(t, http.StatusOK).object(t)
	if mustString(t, uploaded["username"]) != "avatar-member" ||
		jsonInt64(t, uploaded["avatar_version"]) != 1 {
		t.Fatalf("本人上传头像响应错误: %s", mustJSON(t, uploaded))
	}
	expectAvatarImage(t, fetchUserAvatar(t, member, memberID, 1), "image/png", pngBytes)
	expectAvatarImage(t, fetchUserAvatar(t, admin, memberID, 1), "image/png", pngBytes)
	me = member.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if got := jsonInt64(t, mustMap(t, me["user"])["avatar_version"]); got != 1 {
		t.Fatalf("上传后 me 头像版本 = %d，期望 1", got)
	}
	if got := jsonInt64(t, findMember(t, me, "avatar-member")["avatar_version"]); got != 1 {
		t.Fatalf("上传后家庭成员头像版本 = %d，期望 1", got)
	}

	// The administrator may also replace another member's avatar, and every
	// successful upload bumps the version used for cache busting.
	jpegBytes := avatarJPEG(t)
	uploaded = uploadUserAvatar(t, admin, memberID, "image/jpeg", jpegBytes).
		expect(t, http.StatusOK).object(t)
	if jsonInt64(t, uploaded["avatar_version"]) != 2 {
		t.Fatalf("管理员上传成员头像版本错误: %s", mustJSON(t, uploaded))
	}
	expectAvatarImage(t, fetchUserAvatar(t, member, memberID, 2), "image/jpeg", jpegBytes)
	me = member.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if got := jsonInt64(t, mustMap(t, me["user"])["avatar_version"]); got != 2 {
		t.Fatalf("管理员上传后 me 头像版本 = %d，期望 2", got)
	}
	if got := jsonInt64(t, findMember(t, me, "avatar-member")["avatar_version"]); got != 2 {
		t.Fatalf("管理员上传后家庭成员头像版本 = %d，期望 2", got)
	}

	validationCases := []struct {
		name        string
		contentType string
		data        []byte
	}{
		{
			name:        "SVG",
			contentType: "image/svg+xml",
			data:        []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"></svg>`),
		},
		{
			name:        "截断 PNG",
			contentType: "image/png",
			data:        pngBytes[:len(pngBytes)/2],
		},
		{
			name:        "超过 2MiB",
			contentType: "image/png",
			data:        oversizedAvatarPNG(t),
		},
		{
			name:        "单边超过 4096",
			contentType: "image/png",
			data:        oversizedDimensionPNG(t),
		},
	}
	for _, testCase := range validationCases {
		t.Run(testCase.name, func(t *testing.T) {
			expectAvatarFieldError(t,
				uploadUserAvatar(t, member, memberID, testCase.contentType, testCase.data))
		})
	}
	me = member.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if got := jsonInt64(t, mustMap(t, me["user"])["avatar_version"]); got != 2 {
		t.Fatalf("非法上传应保持头像版本 2，实际 %d", got)
	}
	expectAvatarImage(t, fetchUserAvatar(t, member, memberID, 2), "image/jpeg", jpegBytes)

	// Authentication comes before mutation and CSRF checks, and a valid session
	// still needs the CSRF token and a same-origin request.
	anonymous := newRawClient(t, env)
	uploadUserAvatar(t, anonymous, memberID, "image/png", pngBytes).expect(t, http.StatusUnauthorized)
	uploadUserAvatar(t, member, memberID, "image/png", pngBytes, withoutCSRF).
		expect(t, http.StatusForbidden)
	uploadUserAvatar(t, member, memberID, "image/png", pngBytes,
		withOrigin("https://evil.example")).expect(t, http.StatusForbidden)
	fetchUserAvatar(t, anonymous, memberID, 2).expect(t, http.StatusUnauthorized)

	// A member without an avatar, and a member outside the caller's household,
	// are both reported as absent.
	bob := admin.createUser(t, "avatar-bob", strongPassword)
	bobID := jsonInt64(t, bob["id"])
	fetchUserAvatar(t, admin, bobID, 0).expect(t, http.StatusNotFound)
	uploadUserAvatar(t, member, bobID, "image/png", pngBytes).expect(t, http.StatusForbidden)
	fetchUserAvatar(t, member, bobID, 0).expect(t, http.StatusNotFound)
	users = admin.array(t, http.MethodGet, "/api/users/", nil, http.StatusOK)
	if got := jsonInt64(t, findListedUser(t, users, "avatar-bob")["avatar_version"]); got != 0 {
		t.Fatalf("被拒绝的越权上传不应改变头像版本，实际 %d", got)
	}

	_, outsiderID := env.seedUser(t, "头像外家", "avatar-outsider", strongPassword, false)
	outsider := env.loginUser(t, "avatar-outsider", strongPassword)
	fetchUserAvatar(t, outsider, memberID, 2).expect(t, http.StatusNotFound)
	uploadUserAvatar(t, outsider, memberID, "image/png", pngBytes).expect(t, http.StatusNotFound)
	fetchUserAvatar(t, admin, outsiderID, 0).expect(t, http.StatusNotFound)
	uploadUserAvatar(t, admin, outsiderID, "image/png", pngBytes).expect(t, http.StatusNotFound)
}

func expectAvatarImage(t *testing.T, response avatarResponse, contentType string, want []byte) {
	t.Helper()
	if response.status != http.StatusOK {
		t.Fatalf("读取头像状态码 = %d，期望 200；响应: %s", response.status, string(response.body))
	}
	if got := response.header.Get("Content-Type"); got != contentType {
		t.Fatalf("头像 Content-Type = %q，期望 %q", got, contentType)
	}
	if got := response.header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("头像 X-Content-Type-Options = %q，期望 nosniff", got)
	}
	if got := response.header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("头像 Cache-Control = %q，期望包含 no-store", got)
	}
	if !bytes.Equal(response.body, want) {
		t.Fatalf("头像字节与上传内容不一致: got %d bytes, want %d bytes",
			len(response.body), len(want))
	}
}

func TestUserDeletionPreservesHistory(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	adminMe := admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	adminID := jsonInt64(t, mustMap(t, adminMe["user"])["id"])
	adminSession := admin.cookieValue("sessionid")

	deleteeCreated := admin.object(t, http.MethodPost, "/api/users/", map[string]any{
		"username": "deletee", "nickname": "将被删除", "password": strongPassword,
	}, http.StatusCreated)
	deleteeID := jsonInt64(t, deleteeCreated["id"])
	deletee := env.loginUser(t, "deletee", strongPassword)
	deleteeToken := deletee.cookieValue("sessionid")

	account := admin.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "共享账户", "opening_balance": "0.00",
	}, http.StatusCreated)
	accountID := jsonInt64(t, account["id"])
	transaction := deletee.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": accountID, "kind": "expense", "amount": "12.34",
		"category": "餐饮", "date": "2026-10-01T12:00:00",
	}, http.StatusCreated)
	transactionID := jsonInt64(t, transaction["id"])
	if mustString(t, transaction["amount"]) != "12.34" ||
		mustString(t, transaction["created_by_name"]) != "将被删除" ||
		jsonInt64(t, transaction["created_by"]) != deleteeID {
		t.Fatalf("删除前流水记录人错误: %s", mustJSON(t, transaction))
	}

	avatarBytes := avatarPNG(t)
	uploadUserAvatar(t, deletee, deleteeID, "image/png", avatarBytes).expect(t, http.StatusOK)
	expectAvatarImage(t, fetchUserAvatar(t, deletee, deleteeID, 1), "image/png", avatarBytes)

	// Capture the password hash a login may already have verified before the
	// account is deleted. Deletion must make that stale verification unable to
	// create a session afterwards.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	staleUser, staleHash, found, err := env.db.FindUserByUsername(ctx, "deletee")
	if err != nil {
		t.Fatalf("读取待删除用户登录记录失败: %v", err)
	}
	if !found || staleUser.ID != deleteeID || staleHash == "" {
		t.Fatalf("待删除用户登录记录错误: found=%t id=%d", found, staleUser.ID)
	}

	bystander := admin.createUser(t, "bystander", strongPassword)
	bystanderID := jsonInt64(t, bystander["id"])
	bystanderClient := env.loginUser(t, "bystander", strongPassword)
	bystanderClient.do(http.MethodDelete, fmt.Sprintf("/api/users/%d/", deleteeID), nil).
		expect(t, http.StatusForbidden)
	stillActive := deletee.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if stillActive["user"] == nil {
		t.Fatal("普通成员越权删除后目标账号不应被删除")
	}

	// Administrators are never deletable, including the only administrator
	// deleting themself.
	admin.do(http.MethodDelete, fmt.Sprintf("/api/users/%d/", adminID), nil).
		expect(t, http.StatusBadRequest)
	adminMe = admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if jsonInt64(t, mustMap(t, adminMe["user"])["id"]) != adminID {
		t.Fatalf("拒绝删除后管理员身份错误: %s", mustJSON(t, adminMe))
	}

	deletion := admin.do(http.MethodDelete, fmt.Sprintf("/api/users/%d/", deleteeID), nil)
	deletion.expect(t, http.StatusNoContent)
	if len(deletion.body) != 0 {
		t.Fatalf("删除成功响应不应包含 body: %s", string(deletion.body))
	}

	// The row is soft-deleted: the account remains for history joins while all
	// lookups, sessions and usernames treat it as gone.
	if got := env.scalarInt(t, "SELECT count(*) FROM users WHERE id = $1", deleteeID); got != 1 {
		t.Fatalf("删除后用户物理行数 = %d，期望 1", got)
	}
	if got := env.scalarInt(t,
		"SELECT count(*) FROM users WHERE id = $1 AND deleted_at IS NOT NULL", deleteeID); got != 1 {
		t.Fatalf("删除后用户未标记 deleted_at，计数 %d", got)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", deleteeID); got != 0 {
		t.Fatalf("删除后会话数量 = %d，期望 0", got)
	}

	err = env.db.CreateSession(ctx, staleUser.ID, staleHash,
		[]byte("stale-deleted-user-session"), time.Now().Add(time.Hour))
	if !errors.Is(err, store.ErrPasswordChanged) {
		t.Fatalf("删除后旧哈希创建会话错误 = %v，期望 store.ErrPasswordChanged", err)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", deleteeID); got != 0 {
		t.Fatalf("删除后旧哈希不应创建会话，实际 %d", got)
	}

	replayed := replaySession(t, deletee.base, deleteeToken)
	replayed.expect(t, http.StatusOK)
	if replayed.object(t)["user"] != nil {
		t.Fatal("删除后旧 session cookie 不应再识别为登录用户")
	}
	deletee.do(http.MethodGet, "/api/accounts/", nil).expect(t, http.StatusUnauthorized)

	loginAttempt := newTestClient(t, env)
	loginResponse := loginAttempt.do(http.MethodPost, "/api/auth/login/", map[string]any{
		"username": "deletee", "password": strongPassword,
	}).expect(t, http.StatusBadRequest)
	if detail := mustString(t, loginResponse.object(t)["detail"]); detail != "用户名或密码错误。" {
		t.Fatalf("已删除账号登录 detail = %q，期望通用凭据错误", detail)
	}

	adminMe = admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if memberUsernames(t, adminMe)["deletee"] {
		t.Fatalf("me.members 不应包含已删除账号: %s", mustJSON(t, adminMe))
	}
	users := admin.array(t, http.MethodGet, "/api/users/", nil, http.StatusOK)
	if listedUserExists(t, users, "deletee") {
		t.Fatalf("用户列表不应包含已删除账号: %s", mustJSON(t, users))
	}
	if !listedUserExists(t, users, "bystander") {
		t.Fatalf("用户列表应保留其他 active 成员: %s", mustJSON(t, users))
	}
	fetchUserAvatar(t, admin, deleteeID, 1).expect(t, http.StatusNotFound)

	// History written by the deleted member remains visible with its amount
	// and recorder identity intact.
	transactions := admin.array(t, http.MethodGet,
		"/api/transactions/?month=2026-10", nil, http.StatusOK)
	var preserved map[string]any
	for _, item := range transactions {
		if jsonInt64(t, item["id"]) == transactionID {
			preserved = item
		}
	}
	if preserved == nil {
		t.Fatalf("删除成员后其流水消失: %s", mustJSON(t, transactions))
	}
	if mustString(t, preserved["amount"]) != "12.34" ||
		mustString(t, preserved["created_by_name"]) != "将被删除" ||
		jsonInt64(t, preserved["created_by"]) != deleteeID {
		t.Fatalf("删除成员后流水金额或记录人错误: %s", mustJSON(t, preserved))
	}
	if got := env.scalarInt(t,
		"SELECT amount_cents FROM transactions WHERE id = $1", transactionID); got != 1234 {
		t.Fatalf("数据库中流水金额 = %d，期望 1234", got)
	}
	if got := int64(env.scalarInt(t,
		"SELECT created_by FROM transactions WHERE id = $1", transactionID)); got != deleteeID {
		t.Fatalf("数据库中流水记录人 = %d，期望 %d", got, deleteeID)
	}

	// The username stays reserved so a new account cannot inherit old history.
	duplicate := admin.do(http.MethodPost, "/api/users/", map[string]any{
		"username": "deletee", "password": strongPassword,
	}).expect(t, http.StatusBadRequest)
	if _, exists := duplicate.object(t)["username"]; !exists {
		t.Fatalf("已删除账号的用户名应仍被占用: %s", string(duplicate.body))
	}

	bystanderMe := bystanderClient.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if jsonInt64(t, mustMap(t, bystanderMe["user"])["id"]) != bystanderID {
		t.Fatalf("删除其他成员不应影响旁观成员会话: %s", mustJSON(t, bystanderMe))
	}
	if admin.cookieValue("sessionid") != adminSession {
		t.Fatal("删除成员不应替换管理员会话")
	}
	adminMe = admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if jsonInt64(t, mustMap(t, adminMe["user"])["id"]) != adminID {
		t.Fatalf("删除成员后管理员身份错误: %s", mustJSON(t, adminMe))
	}
}
