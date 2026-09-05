package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestProgramRuntime 用真实 tea.Program + 管道 IO 验证运行时按键投递与任务执行。
func TestProgramRuntime(t *testing.T) {
	testArchiveAvailable(t)
	tmp := t.TempDir()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inW.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() { // 排空输出
		buf := make([]byte, 8192)
		for {
			if _, err := outR.Read(buf); err != nil {
				return
			}
		}
	}()

	m := New(testArchive, "test")
	p := tea.NewProgram(m, tea.WithInput(inR), tea.WithOutput(outW))
	m.SetProgram(p)

	go func() {
		time.Sleep(300 * time.Millisecond)
		inW.WriteString("\r") // 详情 → 确认
		time.Sleep(300 * time.Millisecond)
		inW.WriteString("\r") // 确认 → 执行
		time.Sleep(2 * time.Second)
		inW.WriteString("q") // 结果 → 详情
		time.Sleep(300 * time.Millisecond)
		inW.WriteString("q") // 详情 → 退出
	}()

	done := make(chan struct{})
	go func() { _, _ = p.Run(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("程序 10s 未退出（疑似卡死）")
	}

	outPath := filepath.Join(tmp, "dec.zip")
	mm := m
	_ = mm
	// 输出路径由 ResolveOutput 生成：与输入同目录、_decrypted.zip
	defaultOut := filepath.Join(filepath.Dir(testArchive), "ESfjmffkJN0=_decrypted.zip")
	_ = outPath
	if _, err := os.Stat(defaultOut); err != nil {
		t.Fatalf("任务未产生输出文件 %s: %v", defaultOut, err)
	}
	os.Remove(defaultOut)
}
