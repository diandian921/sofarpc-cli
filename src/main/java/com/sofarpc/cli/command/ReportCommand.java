package com.sofarpc.cli.command;

import com.sofarpc.cli.core.ExitCodes;
import com.sofarpc.cli.core.JacksonHolder;
import com.sofarpc.cli.model.BatchResult;
import com.sofarpc.cli.output.ReportGenerator;
import picocli.CommandLine.Command;
import picocli.CommandLine.Option;

import java.io.File;
import java.util.concurrent.Callable;

/**
 * Report command: generate markdown or html report from batch result JSON.
 *
 * @author wuweihua
 */
@Command(name = "report", mixinStandardHelpOptions = true,
    description = "Generate test report from batch result JSON.")
public class ReportCommand implements Callable<Integer> {

    @Option(names = "--input", required = true, description = "Path to batch result JSON file.")
    private File input;

    @Option(names = "--format", description = "Report format: markdown or html.",
        defaultValue = "markdown")
    private String format;

    @Option(names = "--output", description = "Output directory for the report.",
        defaultValue = "./reports/")
    private File outputDir;

    private static final String FORMAT_MARKDOWN = "markdown";
    private static final String FORMAT_HTML = "html";

    @Override
    public Integer call() {
        if (!FORMAT_MARKDOWN.equalsIgnoreCase(format) && !FORMAT_HTML.equalsIgnoreCase(format)) {
            System.err.println("❌ 不支持的格式: " + format + "，仅支持 markdown 或 html");
            return ExitCodes.BAD_ARGS;
        }

        if (!input.exists()) {
            System.err.println("❌ 输入文件不存在: " + input.getPath());
            return ExitCodes.BAD_ARGS;
        }

        try {
            BatchResult batchResult = JacksonHolder.MAPPER.readValue(
                input, BatchResult.class);

            String content;
            if ("html".equalsIgnoreCase(format)) {
                content = ReportGenerator.generateHtml(batchResult);
            } else {
                content = ReportGenerator.generateMarkdown(batchResult);
            }

            ReportGenerator.writeToFile(content, outputDir, format);
        } catch (Exception e) {
            System.err.println("❌ 报告生成失败: " + e.getMessage());
            return ExitCodes.BAD_ARGS;
        }
        return ExitCodes.SUCCESS;
    }
}
