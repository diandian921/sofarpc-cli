package com.sofarpc.cli;

import com.sofarpc.cli.command.BatchCommand;
import com.sofarpc.cli.command.InvokeCommand;
import com.sofarpc.cli.command.PingCommand;
import com.sofarpc.cli.command.ReportCommand;
import com.sofarpc.cli.command.ServerCommand;
import com.sofarpc.cli.core.RpcClientFactory;
import picocli.CommandLine;

import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * SofaRPC CLI entry point.
 *
 * @author wuweihua
 */
@CommandLine.Command(
    name = "sofarpc",
    version = "sofarpc 1.0.0",
    mixinStandardHelpOptions = true,
    description = "SofaRPC command line tool for service testing and verification.",
    subcommands = {
        ServerCommand.class,
        PingCommand.class,
        InvokeCommand.class,
        BatchCommand.class,
        ReportCommand.class
    }
)
public class Main implements Runnable {

    public static void main(String[] args) {
        // Suppress all framework logs to keep stdout clean for JSON parsing
        System.setProperty("com.alipay.sofa.rpc.log.level", "ERROR");
        System.setProperty("logging.level.root", "ERROR");
        System.setProperty("sofa.middleware.log.internal.level", "ERROR");
        System.setProperty("com.alipay.remoting.client.log.level", "ERROR");
        // Suppress Hessian JUL warnings (e.g. "coder field not found")
        Logger.getLogger("com.caucho").setLevel(Level.OFF);
        Logger.getLogger("").setLevel(Level.OFF);

        int exitCode = new CommandLine(new Main()).execute(args);

        // Release all cached RPC connections
        RpcClientFactory.destroyAll();

        System.exit(exitCode);
    }

    @Override
    public void run() {
        new CommandLine(this).usage(System.out);
    }
}
