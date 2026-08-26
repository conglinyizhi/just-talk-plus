// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
)

// diagnoseChunkMs 与运行时音频块粒度一致（约 200ms）。
const diagnoseChunkMs = 200

// lvl 是一块音频的电平（RMS/峰值，归一化 0..1）。
type lvl struct{ rms, peak float64 }

// GenerateDiagnosticReport 用最近一条「有录音文件」的历史生成参数诊断报告：
// 逐块（200ms）统计 RMS/峰值，得到底噪、说话电平与停顿分布等特征，连同当前
// 配置与给大模型的分析 prompt 写入 markdown 文件；返回一行摘要与文件路径。
// 仅生成报告，不修改任何配置。
func GenerateDiagnosticReport(vc config.VoiceConfig) (summary, path string, err error) {
	recPath := latestRecordingPath()
	if recPath == "" {
		return "", "", fmt.Errorf("没有可用的录音：请先在设置中开启「保留录音文件」并录制一段话")
	}
	pcm, rate, err := readWAV(recPath)
	if err != nil {
		return "", "", fmt.Errorf("读取录音失败: %w", err)
	}
	if len(pcm) < 2 {
		return "", "", fmt.Errorf("录音内容为空")
	}
	f := analyzeSpeechFeatures(pcm, rate)
	report := buildDiagnosticReport(recPath, f, vc)
	path, err = writeDiagnosticReport(report)
	if err != nil {
		return "", "", err
	}
	summary = fmt.Sprintf(
		"📋 诊断报告: 底噪 %.3f | 说话 RMS 中位 %.3f | 停顿中位 %s / P90 %s | 峰值 %.2f | %s",
		f.noiseFloorRMS, f.speechRMS, ms(f.gapMedianMs), ms(f.gapP90Ms), f.speechPeak, path)
	return summary, path, nil
}

// latestRecordingPath 返回历史中最近一条带录音文件的记录路径。
func latestRecordingPath() string {
	for _, entry := range History() {
		if entry.RecordingPath != "" {
			return entry.RecordingPath
		}
	}
	return ""
}

// readWAV 解析标准 WAV（RIFF/WAVE，单声道 16-bit PCM），返回裸 PCM 与采样率。
func readWAV(path string) (pcm []byte, sampleRate int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("不是有效的 WAV 文件")
	}
	pos := 12
	for pos+8 <= len(data) {
		id := string(data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		body := pos + 8
		switch id {
		case "fmt ":
			if body+16 > len(data) {
				return nil, 0, fmt.Errorf("WAV fmt 块不完整")
			}
			audioFormat := binary.LittleEndian.Uint16(data[body : body+2])
			channels := binary.LittleEndian.Uint16(data[body+2 : body+4])
			sampleRate = int(binary.LittleEndian.Uint32(data[body+4 : body+8]))
			if audioFormat != 1 || channels != 1 {
				return nil, 0, fmt.Errorf("仅支持单声道 16-bit PCM，实际 fmt=%d channels=%d", audioFormat, channels)
			}
		case "data":
			if body+size > len(data) {
				size = len(data) - body
			}
			return data[body : body+size], sampleRate, nil
		}
		pos = body + size + (size & 1) // chunk 按 2 字节对齐
	}
	return nil, 0, fmt.Errorf("WAV 中没有 data 块")
}

type speechFeatures struct {
	durationMs    int
	noiseFloorRMS float64 // 底噪估计（静音块 RMS 中位）
	speechRMS     float64 // 说话块 RMS 中位
	speechPeak    float64 // 说话块峰值 P90
	peakMax       float64 // 全程峰值最大值（接近 1 说明削波风险）
	gapsMs        []int   // 停顿（≥400ms 静音段）时长
	gapMedianMs   int
	gapP90Ms      int
	speechRatio   float64 // 说话时长占比 0..1
	chunkCount    int
}

// analyzeSpeechFeatures 把 PCM 按 diagnoseChunkMs 分块，用 RMS 序列做
// 说话/静音分割并统计底噪、说话电平与停顿分布。
func analyzeSpeechFeatures(pcm []byte, sampleRate int) speechFeatures {
	chunkBytes := sampleRate * 2 * diagnoseChunkMs / 1000 // 16-bit mono
	if chunkBytes < 2 {
		chunkBytes = 2
	}
	var levels []lvl
	for i := 0; i < len(pcm); i += chunkBytes {
		end := i + chunkBytes
		if end > len(pcm) {
			end = len(pcm)
		}
		peak, rms := analyzePCM(pcm[i:end])
		levels = append(levels, lvl{rms: rms, peak: peak})
	}

	f := speechFeatures{
		durationMs: len(pcm) * 1000 / (sampleRate * 2),
		chunkCount: len(levels),
	}
	// 底噪：全部块 RMS 的 P30（环境本底）。
	noiseFloor := percentileRMS(levels, 0.30)
	f.noiseFloorRMS = noiseFloor

	// 说话/静音分割阈值：底噪 × 2.5（与手动底噪校准一致），下限 0.005。
	vad := noiseFloor * 2.5
	if vad < 0.005 {
		vad = 0.005
	}
	if vad > 0.2 {
		vad = 0.2
	}

	var speechRMS, speechPeak []float64
	var gapStart = -1 // 静音段起始块下标（仅统计说话之间的停顿，开头/结尾静音不计）
	speechSeen := false
	for i, l := range levels {
		if l.rms >= vad {
			speechSeen = true
			speechRMS = append(speechRMS, l.rms)
			speechPeak = append(speechPeak, l.peak)
			if l.peak > f.peakMax {
				f.peakMax = l.peak
			}
			if gapStart >= 0 {
				// 静音段结束：≥400ms 才算停顿
				gapLen := i - gapStart
				if gapLen >= 2 {
					f.gapsMs = append(f.gapsMs, gapLen*diagnoseChunkMs)
				}
				gapStart = -1
			}
		} else if speechSeen && gapStart < 0 {
			gapStart = i
		}
	}
	if len(speechRMS) > 0 {
		sort.Float64s(speechRMS)
		f.speechRMS = speechRMS[len(speechRMS)/2]
		sort.Float64s(speechPeak)
		f.speechPeak = speechPeak[int(float64(len(speechPeak)-1)*0.9)]
		f.speechRatio = float64(len(speechRMS)) / float64(len(levels))
	}
	if len(f.gapsMs) > 0 {
		sort.Ints(f.gapsMs)
		f.gapMedianMs = f.gapsMs[len(f.gapsMs)/2]
		f.gapP90Ms = f.gapsMs[int(float64(len(f.gapsMs)-1)*0.9)]
	}
	return f
}

// percentileRMS 取全部块 RMS 的指定分位。
func percentileRMS(levels []lvl, q float64) float64 {
	if len(levels) == 0 {
		return 0
	}
	vals := make([]float64, len(levels))
	for i, l := range levels {
		vals[i] = l.rms
	}
	sort.Float64s(vals)
	return vals[int(float64(len(vals)-1)*q)]
}

// buildDiagnosticReport 生成 markdown 报告：特征 + 当前配置 + 给大模型的 prompt。
func buildDiagnosticReport(recPath string, f speechFeatures, vc config.VoiceConfig) string {
	var b strings.Builder
	b.WriteString("# Just Talk Plus 参数诊断报告\n\n")
	fmt.Fprintf(&b, "生成时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "录音文件: `%s`（%.1fs，%d 块 × %dms）\n\n", recPath, float64(f.durationMs)/1000, f.chunkCount, diagnoseChunkMs)

	b.WriteString("## 音频特征（16-bit PCM，数值均为归一化幅值 0..1）\n\n")
	fmt.Fprintf(&b, "- 底噪 RMS（安静本底）: **%.4f**（%.1f dBFS）\n", f.noiseFloorRMS, toDB(f.noiseFloorRMS))
	fmt.Fprintf(&b, "- 说话段 RMS 中位: **%.4f**（%.1f dBFS）\n", f.speechRMS, toDB(f.speechRMS))
	fmt.Fprintf(&b, "- 说话段峰值 P90: %.4f（%.1f dBFS）\n", f.speechPeak, toDB(f.speechPeak))
	fmt.Fprintf(&b, "- 全程峰值最大值: %.4f%s\n", f.peakMax, peakWarn(f.peakMax))
	if len(f.gapsMs) > 0 {
		fmt.Fprintf(&b, "- 停顿数量: %d 次；时长: 中位 %s / P90 %s / 最长 %s\n",
			len(f.gapsMs), ms(f.gapMedianMs), ms(f.gapP90Ms), ms(f.gapsMs[len(f.gapsMs)-1]))
	} else {
		b.WriteString("- 停顿数量: 0（录音内没有 ≥400ms 的静音停顿）\n")
	}
	fmt.Fprintf(&b, "- 说话时长占比: %.0f%%\n\n", f.speechRatio*100)

	b.WriteString("## 当前配置\n\n")
	fmt.Fprintf(&b, "- `stop_delay_ms` = %d（停止静音等待）\n", vc.StopDelayMs)
	fmt.Fprintf(&b, "- `noise_threshold` = %.4f（底噪阈值 RMS）\n", vc.NoiseThreshold)
	fmt.Fprintf(&b, "- `noise_deadband` = %.4f（停止判定容差）\n", vc.NoiseDeadband)
	fmt.Fprintf(&b, "- `noise_floor_auto` = %v（自动底噪）\n", vc.NoiseFloorAuto)
	fmt.Fprintf(&b, "- AGC 目标峰值 = %.3f / 峰值上限 = %.3f / 最大增益 = %d\n\n",
		agcOr(vc.AutoGainTargetPeak, 0.42), agcOr(vc.AutoGainMaxPeak, 0.65), agcOrInt(vc.AutoGainMaxGain, 25))

	b.WriteString("## 请大模型分析\n\n")
	b.WriteString("把下面这段发给大模型，让它基于以上真实特征给出参数调整建议：\n\n")
	b.WriteString("```\n")
	b.WriteString("你是 Just Talk Plus（桌面语音输入工具）的参数调优助手。\n")
	b.WriteString("用户感觉体验有问题（如：丢末尾字 / 识别开始晚 / 等待停止太久）。请根据\n")
	b.WriteString("音频特征与当前配置，给出每个参数的建议值与理由。\n\n")
	b.WriteString("参数语义与安全范围：\n")
	b.WriteString("- noise_threshold（底噪阈值 RMS，0..0.2）：应夹在「底噪」与「说话声」之间；环境噪声误触发则调高，轻声说话被当噪音则调低。\n")
	b.WriteString("- noise_deadband（停止判定容差，0..0.2）：调太大会把正常说话判成噪音导致提前截断丢字，建议不超过 0.1。\n")
	b.WriteString("- noise_floor_auto（自动底噪，bool）：环境底噪不稳定时开启。\n")
	b.WriteString("- stop_delay_ms（静音停止等待，200..60000）：建议 ≥ 停顿 P90，宁可略长避免截断；用户手动停止时不需太长。\n")
	b.WriteString("- auto_gain_target_peak / auto_gain_max_peak（AGC 目标/上限峰值，0..1）：说话段峰值长期低于 0.3 可上调目标，接近 0.9 说明削波风险应下调。\n\n")
	b.WriteString("只输出 JSON（不要 markdown 代码块）：\n")
	b.WriteString("{\"建议\": {\"noise_threshold\": 0.xx, \"noise_deadband\": 0.xx, \"noise_floor_auto\": bool, \"stop_delay_ms\": xxx, \"auto_gain_target_peak\": 0.xx, \"auto_gain_max_peak\": 0.xx}, \"理由\": \"...\"}\n")
	b.WriteString("```\n")
	return b.String()
}

// writeDiagnosticReport 把报告写入录音同目录，返回路径。
func writeDiagnosticReport(report string) (string, error) {
	dir := recordingDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("创建报告目录失败: %w", err)
	}
	path := filepath.Join(dir, "diagnostic-report-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".md")
	if err := os.WriteFile(path, []byte(report), 0600); err != nil {
		return "", fmt.Errorf("写报告失败: %w", err)
	}
	return path, nil
}

func ms(v int) string { return strconv.Itoa(v) + "ms" }

func toDB(v float64) float64 {
	if v <= 0 {
		return -120
	}
	return 20 * math.Log10(v)
}

func peakWarn(peak float64) string {
	switch {
	case peak >= 0.98:
		return " ⚠️ 接近削波（>0.98）"
	case peak >= 0.9:
		return " ⚠️ 偏高（>0.9）"
	case peak < 0.2:
		return "（偏低）"
	}
	return ""
}

func agcOr(v *float64, def float64) float64 {
	if v == nil {
		return def
	}
	return *v
}

func agcOrInt(v *int, def int) int {
	if v == nil {
		return def
	}
	return *v
}
