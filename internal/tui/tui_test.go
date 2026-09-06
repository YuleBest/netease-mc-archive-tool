package tui

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const testArchive = "../../testdata/ESfjmffkJN0=.zip"

func testArchiveAvailable(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(testArchive); err != nil {
		t.Skipf("测试存档不存在，跳过: %v", err)
	}
}

func keyEnter() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

// runCmd 递归执行 Cmd（展开 tea.Batch），收集全部消息。
func runCmd(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if bm, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range bm {
			out = append(out, runCmd(t, c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func findJobMsg(t *testing.T, msgs []tea.Msg) jobMsg {
	t.Helper()
	for _, m := range msgs {
		if jm, ok := m.(jobMsg); ok {
			return jm
		}
	}
	t.Fatalf("消息中未找到 jobMsg: %T", msgs)
	return jobMsg{}
}
func keyDown() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyDown} }
func keyEsc() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyEsc} }

// TestNew_LoadsArchive 直接传入路径时应完成加载并停留在详情页。
func TestNew_LoadsArchive(t *testing.T) {
	testArchiveAvailable(t)
	m := New(testArchive, "test")
	if m.state != stateDetail {
		t.Fatalf("初始状态 = %v, 期望 stateDetail", m.state)
	}
	if m.stats == nil || m.info == nil {
		t.Fatal("存档信息未加载")
	}
	if m.info.EngineVersion != "1.21.120" {
		t.Fatalf("引擎版本 = %q", m.info.EngineVersion)
	}
	if string(m.key) != "88329851" {
		t.Fatalf("预推导密钥 = %q", m.key)
	}
}

// TestInput_PathError 无效路径应停留在输入页并展示错误。
func TestInput_PathError(t *testing.T) {
	m := New("", "test")
	m.input.SetValue("/nonexistent/path/archive.zip")
	m2, _ := m.Update(keyEnter())
	mm := m2.(Model)
	if mm.state != stateInput || mm.err == nil {
		t.Fatalf("无效路径应报错: state=%v err=%v", mm.state, mm.err)
	}
}

// TestDecryptFlow_RunsJobAndWritesOutput 从详情页走完 解密→确认→执行→结果 全流程。
func TestDecryptFlow_RunsJobAndWritesOutput(t *testing.T) {
	testArchiveAvailable(t)
	tmp := t.TempDir()
	m := New(testArchive, "test")
	m.cursor = actDecrypt

	// 详情 → 确认
	m2, _ := m.Update(keyEnter())
	mm := m2.(Model)
	if mm.state != stateConfirm {
		t.Fatalf("状态 = %v, 期望 stateConfirm", mm.state)
	}
	// 覆盖输出路径到临时目录
	mm.outPath = filepath.Join(tmp, "dec.zip")
	mm.asZip = true
	mm.outExists = false

	// 确认 → 执行（执行返回的 Cmd 同步跑完任务）
	m3, cmd := mm.Update(keyEnter())
	m3m := m3.(Model)
	if m3m.state != stateRun {
		t.Fatalf("状态 = %v, 期望 stateRun", m3m.state)
	}
	if cmd == nil {
		t.Fatal("应返回任务 Cmd")
	}
	jm := findJobMsg(t, runCmd(t, cmd))
	if jm.err != nil {
		t.Fatalf("任务失败: %v", jm.err)
	}
	if jm.res == nil || len(jm.res.Transformed) != 2 {
		t.Fatalf("应解密 2 个文件: %+v", jm.res)
	}

	// 结果 → 返回详情
	m4, _ := m3.Update(jm)
	m4m := m4.(Model)
	if m4m.state != stateResult {
		t.Fatalf("状态 = %v, 期望 stateResult", m4m.state)
	}
	m5, _ := m4.Update(keyEnter())
	if m5.(Model).state != stateDetail {
		t.Fatal("Enter 应返回详情页")
	}

	// 校验产物：CURRENT 为已知明文
	zr, err := zip.OpenReader(mm.outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		// decrypt 保留原始条目名（世界根目录不剥离，strip 仅用于 export）
		if f.Name == "ESfjmffkJN0=/db/CURRENT" {
			r, _ := f.Open()
			data, _ := io.ReadAll(r)
			r.Close()
			if !bytes.Equal(data, []byte("MANIFEST-000006\n")) {
				t.Fatalf("CURRENT = %q", data)
			}
			return
		}
	}
	t.Fatal("产物缺少 ESfjmffkJN0=/db/CURRENT")
}

// TestExportFlow_StripsWorldRoot 导出操作应产出根级 level.dat 的 .mcworld。
func TestExportFlow_StripsWorldRoot(t *testing.T) {
	testArchiveAvailable(t)
	tmp := t.TempDir()
	m := New(testArchive, "test")
	m.cursor = actExport

	m2, _ := m.Update(keyEnter())
	mm := m2.(Model)
	if mm.state != stateConfirm {
		t.Fatalf("状态 = %v", mm.state)
	}
	mm.outPath = filepath.Join(tmp, "world.mcworld")
	mm.asZip = true
	mm.outExists = false

	m3, cmd := mm.Update(keyEnter())
	jm := findJobMsg(t, runCmd(t, cmd))
	if jm.err != nil {
		t.Fatalf("任务失败: %v", jm.err)
	}
	_ = m3

	zr, err := zip.OpenReader(mm.outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	root := map[string]bool{}
	for _, f := range zr.File {
		if f.Name == "level.dat" {
			root["level.dat"] = true
		}
		if f.Name == "ESfjmffkJN0=/level.dat" {
			t.Fatal("世界内容不应嵌套在子目录")
		}
	}
	if !root["level.dat"] {
		t.Fatal("level.dat 应位于压缩包根")
	}
}

// TestQuitKeys q/Esc/Ctrl+C 触发退出。
func TestQuitKeys(t *testing.T) {
	m := New("", "test")
	for _, k := range []tea.KeyMsg{keyEsc(), {Type: tea.KeyCtrlC}} {
		m2, cmd := m.Update(k)
		mm := m2.(Model)
		if !mm.quitting || cmd == nil {
			t.Fatalf("%v 应退出", k)
		}
	}
}

// TestView_InputScreen_NoRegression 回归测试：空路径进入输入屏时渲染 View，
// 此前因 bubbles v0.20.0 对 CJK 占位符按显示宽度切片而 panic（slice bounds [:33]）。
func TestView_InputScreen_NoRegression(t *testing.T) {
	m := New("", "test") // 无初始路径 → 输入屏
	if m.state != stateInput {
		t.Fatalf("状态 = %v, 期望 stateInput", m.state)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("渲染输入屏 panic: %v", r)
		}
	}()
	if v := m.View(); v == "" {
		t.Fatal("View 不应为空")
	}
}

// TestView_DetailScreen 渲染详情屏（真实存档）。
func TestView_DetailScreen(t *testing.T) {
	testArchiveAvailable(t)
	m := New(testArchive, "test")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("渲染详情屏 panic: %v", r)
		}
	}()
	if v := m.View(); !bytes.Contains([]byte(v), []byte("1.21.120")) {
		t.Fatalf("详情屏缺少引擎版本:\n%s", v)
	}
}
