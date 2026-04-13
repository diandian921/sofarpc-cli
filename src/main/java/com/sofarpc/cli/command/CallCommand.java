package com.sofarpc.cli.command;

import com.sofarpc.cli.core.ExitCodes;
import com.sofarpc.cli.core.GlobalConfig;
import com.sofarpc.cli.service.ArgParser;
import com.sofarpc.cli.service.AssertionEvaluator;
import com.sofarpc.cli.service.OutputFormatter;
import com.sofarpc.cli.service.RpcInvokeService;
import picocli.CommandLine.Command;
import picocli.CommandLine.Option;
import picocli.CommandLine.Parameters;

import java.util.concurrent.Callable;

/**
 * Shorthand invoke command.
 * Usage: sofarpc call <server> <service/method> [args-json] [options]
 *
 * @author wuweihua
 */
@Command(name = "call", mixinStandardHelpOptions = true,
    description = "Shorthand invoke: call <server> <service/method> [args-json]")
public class CallCommand implements Callable<Integer> {

    @Parameters(index = "0", description = "Server alias.")
    private String server;

    @Parameters(index = "1",
        description = "service/method, e.g. com.example.UserService/getUser")
    private String serviceMethod;

    @Parameters(index = "2", arity = "0..1",
        description = "JSON args. Object for single param, array for multi.")
    private String args;

    @Option(names = "--arg-types",
        description = "Parameter types, comma separated. Required when args is specified.")
    private String argTypes;

    @Option(names = "--assert",
        description = "JSONPath assertion, e.g. '$.status == \"ACTIVE\"'.")
    private String assertExpr;

    @Option(names = "--timeout", description = "Timeout in milliseconds.")
    private Integer timeout;

    @Option(names = "--json", description = "Output in JSON format.")
    private boolean json;

    @Override
    public Integer call() {
        // Parse service/method — the only logic unique to CallCommand
        int slashIdx = serviceMethod.lastIndexOf('/');
        if (slashIdx <= 0 || slashIdx == serviceMethod.length() - 1) {
            OutputFormatter.printError(
                "格式错误，应为 <service/method>，例如 com.example.UserService/getUser",
                ExitCodes.BAD_ARGS, json);
            return ExitCodes.BAD_ARGS;
        }
        String service = serviceMethod.substring(0, slashIdx);
        String method = serviceMethod.substring(slashIdx + 1);

        int effectiveTimeout = timeout != null
            ? timeout : GlobalConfig.getInstance().getTimeout();
        if (effectiveTimeout <= 0) {
            OutputFormatter.printError(
                "timeout 必须为正整数，当前值: " + effectiveTimeout, ExitCodes.BAD_ARGS, json);
            return ExitCodes.BAD_ARGS;
        }

        // Parse arguments
        ArgParser.ParsedArgs parsed;
        try {
            parsed = ArgParser.parse(argTypes, args);
        } catch (Exception e) {
            OutputFormatter.printError("参数解析失败: " + e.getMessage(), ExitCodes.BAD_ARGS, json);
            return ExitCodes.BAD_ARGS;
        }

        // Invoke
        RpcInvokeService invokeService = new RpcInvokeService();
        RpcInvokeService.InvokeResult result = invokeService.invoke(
            server, service, method, parsed.getTypes(), parsed.getValues(), effectiveTimeout);

        if (!result.isSuccess()) {
            OutputFormatter.printInvokeResult(result, null, json);
            return result.getExitCode();
        }

        // Assertion
        AssertionEvaluator.AssertionResult assertion = null;
        if (assertExpr != null && !assertExpr.isEmpty()) {
            assertion = AssertionEvaluator.evaluateExpression(result.getResult(), assertExpr);
        }

        OutputFormatter.printInvokeResult(result, assertion, json);

        if (assertion != null && !assertion.isPassed()) {
            return ExitCodes.INVOKE_FAIL;
        }
        return ExitCodes.SUCCESS;
    }
}
