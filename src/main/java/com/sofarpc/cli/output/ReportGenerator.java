package com.sofarpc.cli.output;

import com.sofarpc.cli.model.BatchResult;
import com.sofarpc.cli.model.CaseResult;

import java.io.File;
import java.io.FileWriter;
import java.io.IOException;
import java.util.List;

/**
 * Generate test reports from batch result JSON.
 * Supports markdown and html formats.
 *
 * @author wuweihua
 */
public class ReportGenerator {

    private ReportGenerator() {
    }

    /**
     * Escape characters that would break a Markdown table cell:
     * - pipe `|` is table column separator
     * - CR/LF breaks the row
     * - backslash escapes itself
     */
    private static String escapeMarkdownCell(String raw) {
        if (raw == null) {
            return "-";
        }
        return raw
            .replace("\\", "\\\\")
            .replace("|", "\\|")
            .replace("\r\n", " ")
            .replace("\n", " ")
            .replace("\r", " ");
    }

    public static String generateMarkdown(BatchResult batchResult) {
        StringBuilder sb = new StringBuilder();
        sb.append("# SofaRPC Batch Test Report\n\n");
        sb.append("- **执行时间**: ").append(batchResult.getStartTime()).append("\n");
        sb.append("- **总计**: ").append(batchResult.getTotal()).append("\n");
        sb.append("- **通过**: ").append(batchResult.getPassed()).append("\n");
        sb.append("- **失败**: ").append(batchResult.getFailed()).append("\n");
        sb.append("- **耗时**: ").append(batchResult.getDuration()).append("\n\n");

        // Pass rate
        int total = batchResult.getTotal();
        int passed = batchResult.getPassed();
        if (total > 0) {
            double rate = (double) passed / total * 100;
            sb.append(String.format("**通过率: %.1f%%**%n%n", rate));
        }

        // Results table
        sb.append("## 用例明细\n\n");
        sb.append("| 用例 | 结果 | 耗时 | 错误 |\n");
        sb.append("|------|------|------|------|\n");

        List<CaseResult> results = batchResult.getResults();
        if (results != null) {
            long totalLatency = 0;
            long maxLatency = Long.MIN_VALUE;
            long minLatency = Long.MAX_VALUE;
            int latencyCount = 0;

            for (CaseResult r : results) {
                String icon = r.isPassed() ? "✅ PASS" : "❌ FAIL";
                String latencyStr = "-";
                if (r.getLatencyMs() != null) {
                    long latency = r.getLatencyMs();
                    latencyStr = latency + "ms";
                    totalLatency += latency;
                    maxLatency = Math.max(maxLatency, latency);
                    minLatency = Math.min(minLatency, latency);
                    latencyCount++;
                }
                String error = r.getError() != null ? r.getError() : "-";
                sb.append("| ").append(escapeMarkdownCell(r.getCaseName()))
                    .append(" | ").append(icon)
                    .append(" | ").append(latencyStr)
                    .append(" | ").append(escapeMarkdownCell(error))
                    .append(" |\n");
            }

            // Latency stats
            if (latencyCount > 0) {
                sb.append("\n## 耗时统计\n\n");
                sb.append("| 指标 | 值 |\n");
                sb.append("|------|------|\n");
                sb.append("| 平均 | ").append(totalLatency / latencyCount).append("ms |\n");
                sb.append("| 最大 | ").append(maxLatency).append("ms |\n");
                sb.append("| 最小 | ").append(minLatency).append("ms |\n");
            }
        }

        return sb.toString();
    }

    public static String generateHtml(BatchResult batchResult) {
        StringBuilder sb = new StringBuilder();
        sb.append("<!DOCTYPE html>\n<html>\n<head>\n");
        sb.append("<meta charset=\"UTF-8\">\n");
        sb.append("<title>SofaRPC Test Report</title>\n");
        sb.append("<style>\n");
        sb.append("body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; max-width: 900px; margin: 40px auto; padding: 0 20px; }\n");
        sb.append("table { border-collapse: collapse; width: 100%; margin: 20px 0; }\n");
        sb.append("th, td { border: 1px solid #ddd; padding: 8px 12px; text-align: left; }\n");
        sb.append("th { background: #f5f5f5; }\n");
        sb.append(".pass { color: #22863a; } .fail { color: #cb2431; }\n");
        sb.append(".summary { background: #f6f8fa; padding: 16px; border-radius: 6px; margin: 20px 0; }\n");
        sb.append("</style>\n</head>\n<body>\n");

        sb.append("<h1>SofaRPC Batch Test Report</h1>\n");

        int total = batchResult.getTotal();
        int passed = batchResult.getPassed();
        int failed = batchResult.getFailed();
        double rate = total > 0 ? (double) passed / total * 100 : 0;

        sb.append("<div class=\"summary\">\n");
        sb.append("<p><strong>执行时间:</strong> ").append(escapeHtml(batchResult.getStartTime())).append("</p>\n");
        sb.append("<p><strong>总计:</strong> ").append(total)
            .append(" | <strong>通过:</strong> <span class=\"pass\">").append(passed).append("</span>")
            .append(" | <strong>失败:</strong> <span class=\"fail\">").append(failed).append("</span>")
            .append(" | <strong>耗时:</strong> ").append(escapeHtml(batchResult.getDuration())).append("</p>\n");
        sb.append(String.format("<p><strong>通过率: %.1f%%</strong></p>%n", rate));
        sb.append("</div>\n");

        sb.append("<h2>用例明细</h2>\n");
        sb.append("<table>\n<tr><th>用例</th><th>结果</th><th>耗时</th><th>错误</th></tr>\n");

        List<CaseResult> results = batchResult.getResults();
        if (results != null) {
            long totalLatency = 0;
            long maxLatency = Long.MIN_VALUE;
            long minLatency = Long.MAX_VALUE;
            int latencyCount = 0;

            for (CaseResult r : results) {
                String cls = r.isPassed() ? "pass" : "fail";
                String label = r.isPassed() ? "PASS" : "FAIL";
                String latencyStr = "-";
                if (r.getLatencyMs() != null) {
                    long latency = r.getLatencyMs();
                    latencyStr = latency + "ms";
                    totalLatency += latency;
                    maxLatency = Math.max(maxLatency, latency);
                    minLatency = Math.min(minLatency, latency);
                    latencyCount++;
                }
                String error = r.getError() != null
                    ? escapeHtml(r.getError()) : "-";
                sb.append("<tr><td>").append(escapeHtml(r.getCaseName()))
                    .append("</td><td class=\"").append(cls).append("\">").append(label)
                    .append("</td><td>").append(latencyStr)
                    .append("</td><td>").append(error)
                    .append("</td></tr>\n");
            }

            if (latencyCount > 0) {
                sb.append("</table>\n");
                sb.append("<h2>耗时统计</h2>\n");
                sb.append("<table>\n<tr><th>指标</th><th>值</th></tr>\n");
                sb.append("<tr><td>平均</td><td>").append(totalLatency / latencyCount).append("ms</td></tr>\n");
                sb.append("<tr><td>最大</td><td>").append(maxLatency).append("ms</td></tr>\n");
                sb.append("<tr><td>最小</td><td>").append(minLatency).append("ms</td></tr>\n");
            }
        }

        sb.append("</table>\n</body>\n</html>");
        return sb.toString();
    }

    public static void writeToFile(String content, File outputDir, String format) throws IOException {
        outputDir.mkdirs();
        String filename = "report." + ("html".equals(format) ? "html" : "md");
        File outputFile = new File(outputDir, filename);
        try (FileWriter writer = new FileWriter(outputFile)) {
            writer.write(content);
        }
        System.out.println("✅ 报告已生成: " + outputFile.getAbsolutePath());
    }

    private static String escapeHtml(String text) {
        if (text == null) {
            return "-";
        }
        return text.replace("&", "&amp;")
            .replace("<", "&lt;")
            .replace(">", "&gt;")
            .replace("\"", "&quot;");
    }
}
