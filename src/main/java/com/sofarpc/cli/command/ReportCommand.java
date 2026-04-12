package com.sofarpc.cli.command;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.sofarpc.cli.output.ReportGenerator;
import picocli.CommandLine.Command;
import picocli.CommandLine.Option;

import java.io.File;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Report command: generate markdown or html report from batch result JSON.
 *
 * @author wuweihua
 */
@Command(name = "report", description = "Generate test report from batch result JSON.")
public class ReportCommand implements Runnable {

    private static final int EXIT_BAD_ARGS = 3;
    private static final ObjectMapper OBJECT_MAPPER = new ObjectMapper();

    @Option(names = "--input", required = true, description = "Path to batch result JSON file.")
    private File input;

    @Option(names = "--format", description = "Report format: markdown or html.", defaultValue = "markdown")
    private String format;

    @Option(names = "--output", description = "Output directory for the report.", defaultValue = "./reports/")
    private File outputDir;

    @Override
    @SuppressWarnings("unchecked")
    public void run() {
        if (!input.exists()) {
            System.err.println("❌ 输入文件不存在: " + input.getPath());
            System.exit(EXIT_BAD_ARGS);
            return;
        }

        try {
            Map<String, Object> batchResult = OBJECT_MAPPER.readValue(input, LinkedHashMap.class);

            String content;
            if ("html".equalsIgnoreCase(format)) {
                content = ReportGenerator.generateHtml(batchResult);
            } else {
                content = ReportGenerator.generateMarkdown(batchResult);
            }

            ReportGenerator.writeToFile(content, outputDir, format);
        } catch (Exception e) {
            System.err.println("❌ 报告生成失败: " + e.getMessage());
            System.exit(EXIT_BAD_ARGS);
        }
    }
}
