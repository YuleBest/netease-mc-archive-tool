package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/YuleBest/netease-mc-archive-tool/internal/archive"
	"github.com/YuleBest/netease-mc-archive-tool/internal/crypt"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Padding(0, 1)
	panelStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(0, 2)
	labelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	okStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	selectedStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	idleStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	progEmptyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	progFullStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// View 实现 tea.Model。
func (m Model) View() string {
	if m.quitting {
		return "再见！\n"
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("nmcat 交互式存档工具") + helpStyle.Render("  v"+m.version) + "\n\n")

	switch m.state {
	case stateInput:
		b.WriteString(m.viewInput())
	case stateDetail:
		b.WriteString(m.viewDetail())
	case stateConfirm:
		b.WriteString(m.viewConfirm())
	case stateRun:
		b.WriteString(m.viewRun())
	case stateResult:
		b.WriteString(m.viewResult())
	}
	return b.String()
}

func (m Model) viewInput() string {
	var b strings.Builder
	b.WriteString("请输入存档路径（网易加密/已解密的 zip 或世界目录）：\n\n")
	b.WriteString(m.input.View() + "\n")
	if m.err != nil {
		b.WriteString("\n" + errStyle.Render("✗ "+m.err.Error()) + "\n")
	}
	b.WriteString("\n" + helpStyle.Render("Enter 确认 · Esc 退出"))
	return b.String()
}

func (m Model) viewDetail() string {
	var b strings.Builder
	if m.err != nil {
		b.WriteString(errStyle.Render("✗ "+m.err.Error()) + "\n\n")
	}
	b.WriteString(m.infoPanel() + "\n")

	b.WriteString("选择操作：\n\n")
	for i, label := range actionLabels {
		if i == m.cursor {
			b.WriteString(selectedStyle.Render("  ▸ "+label) + "\n")
		} else {
			b.WriteString(idleStyle.Render("    "+label) + "\n")
		}
	}
	b.WriteString("\n" + helpStyle.Render("↑/↓ 选择 · Enter 确认 · q/Esc 退出"))
	return b.String()
}

func (m Model) infoPanel() string {
	st, info := m.stats, m.info
	var lines []string
	kv := func(k, v string) { lines = append(lines, labelStyle.Render(pad(k, 14))+v) }

	world := "未知"
	switch {
	case st != nil && st.WorldRoot != "" && st.WorldRoot != ".":
		world = strings.Split(st.WorldRoot, "/")[0]
	case m.store != nil && st != nil && st.Kind == "dir":
		world = filepath.Base(filepath.Clean(m.path))
	}
	kv("世界", world)
	if info != nil {
		kv("世界名", orDash(info.LevelName))
		kv("MC 引擎版本", orDash(info.EngineVersion))
		kv("最后游玩", orDash(formatTime(info.LastPlayed)))
		kv("游戏模式", gameTypeName(info.GameType))
		kv("难度", difficultyName(info.Difficulty))
	}
	if st != nil {
		kv("文件", fmt.Sprintf("%d 个 / %s", st.FileCount, humanSize(st.TotalSize)))
		switch {
		case len(st.EncryptedDB) > 0:
			kv("db 加密", okStyle.Render(fmt.Sprintf("已加密（%d/%d 个文件带魔数）", len(st.EncryptedDB), st.DBFiles)))
		case st.DBFiles > 0:
			kv("db 加密", "未加密")
		}
	}
	switch {
	case m.key != nil:
		kv("密钥", okStyle.Render(fmt.Sprintf("可自动推导（%s）", crypt.FormatKeyASCII(m.key))))
	case m.keyErr != nil:
		kv("密钥", errStyle.Render("推导失败（存档可能损坏或为资源中心二次加密）"))
	}
	return panelStyle.Render(strings.Join(lines, "\n"))
}

func (m Model) viewConfirm() string {
	action := ""
	switch m.mode {
	case archive.ModeEncrypt:
		action = "加密存档（国际版 → 网易版）"
	default:
		if m.cursor == actExport {
			action = "导出 .mcworld（国际版一键导入）"
		} else {
			action = "解密存档（网易版 → 国际版）"
		}
	}
	var b strings.Builder
	b.WriteString("即将执行：\n\n")
	lines := []string{
		labelStyle.Render(pad("操作", 14)) + action,
		labelStyle.Render(pad("输入", 14)) + m.path,
		labelStyle.Render(pad("输出", 14)) + m.outPath,
	}
	if m.outExists {
		lines = append(lines, warnStyle.Render("⚠ 输出已存在，确认后将覆盖"))
	}
	if m.mode == archive.ModeEncrypt {
		lines = append(lines, labelStyle.Render(pad("密钥", 14))+crypt.FormatKeyASCII(crypt.DefaultKey)+"（官方默认）")
	} else if m.key != nil {
		lines = append(lines, labelStyle.Render(pad("密钥", 14))+crypt.FormatKeyASCII(m.key)+"（自动推导）")
	}
	b.WriteString(panelStyle.Render(strings.Join(lines, "\n")) + "\n\n")
	b.WriteString(helpStyle.Render("Enter 开始 · Esc 返回"))
	return b.String()
}

func (m Model) viewRun() string {
	spinner := ""
	if m.frame < len(spinnerFrames) {
		spinner = spinnerFrames[m.frame]
	}
	bar := ""
	if m.progressTotal > 0 {
		const width = 30
		filled := m.progressDone * width / m.progressTotal
		bar = progFullStyle.Render(strings.Repeat("█", filled)) + progEmptyStyle.Render(strings.Repeat("░", width-filled))
	} else {
		bar = progEmptyStyle.Render(strings.Repeat("░", 30))
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s 正在处理… %s\n\n", spinner, bar))
	if m.progressName != "" && m.progressTotal > 0 {
		b.WriteString(fmt.Sprintf("  [%d/%d] %s\n", m.progressDone, m.progressTotal, m.progressName))
	}
	b.WriteString("\n" + helpStyle.Render("Ctrl+C 取消"))
	return b.String()
}

func (m Model) viewResult() string {
	if m.jobErr != nil {
		var b strings.Builder
		b.WriteString(errStyle.Render("✗ 操作失败") + "\n\n")
		b.WriteString(errStyle.Render(m.jobErr.Error()) + "\n")
		b.WriteString("\n" + helpStyle.Render("常见原因见 README FAQ；Enter 返回详情 · q 退出"))
		return b.String()
	}
	res := m.result
	var b strings.Builder
	b.WriteString(okStyle.Render("✓ 操作完成") + "\n\n")
	lines := []string{
		labelStyle.Render(pad("输出", 14)) + m.outPath,
		labelStyle.Render(pad("条目", 14)) + fmt.Sprintf("%d 个", res.Copied),
	}
	if len(res.Transformed) > 0 {
		lines = append(lines, labelStyle.Render(pad("已处理", 14))+fmt.Sprintf("%d 个：%s", len(res.Transformed), strings.Join(res.Transformed, ", ")))
	}
	if res.Verified != "" {
		lines = append(lines, labelStyle.Render(pad("校验", 14))+okStyle.Render(res.Verified))
	}
	b.WriteString(panelStyle.Render(strings.Join(lines, "\n")) + "\n\n")
	b.WriteString(helpStyle.Render("Enter 返回详情 · q 退出"))
	return b.String()
}

// ---------------------------------------------------------------- 小工具

func pad(s string, n int) string {
	if len([]rune(s)) >= n {
		return s + " "
	}
	return s + strings.Repeat(" ", n-len([]rune(s)))
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func gameTypeName(v int32) string {
	switch v {
	case 0:
		return "生存"
	case 1:
		return "创造"
	case 2:
		return "冒险"
	case 3:
		return "旁观"
	}
	return "未知"
}

func difficultyName(v int32) string {
	switch v {
	case 0:
		return "和平"
	case 1:
		return "简单"
	case 2:
		return "普通"
	case 3:
		return "困难"
	}
	return "未知"
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d 字节", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func formatTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04")
}
