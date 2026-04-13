package com.sofarpc.cli.command;

import com.sofarpc.cli.core.ExitCodes;
import com.sofarpc.cli.core.ServerStore;
import com.sofarpc.cli.core.ServerStore.ServerEntry;
import com.sofarpc.cli.output.JsonPrinter;
import com.sofarpc.cli.service.OutputFormatter;
import picocli.CommandLine.Command;
import picocli.CommandLine.Option;
import picocli.CommandLine.Parameters;

import java.io.File;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.Callable;

/**
 * Server alias management: add, list, remove, export, import.
 *
 * @author wuweihua
 */
@Command(
    name = "server",
    mixinStandardHelpOptions = true,
    description = "Manage server aliases.",
    subcommands = {
        ServerCommand.Add.class,
        ServerCommand.List.class,
        ServerCommand.Remove.class,
        ServerCommand.Export.class,
        ServerCommand.Import.class
    }
)
public class ServerCommand implements Callable<Integer> {

    @Override
    public Integer call() {
        new picocli.CommandLine(this).usage(System.out);
        return ExitCodes.SUCCESS;
    }

    @Command(name = "add", mixinStandardHelpOptions = true,
        description = "Add or update a server alias.")
    static class Add implements Callable<Integer> {

        @Parameters(index = "0", description = "Server alias name.")
        private String name;

        @Parameters(index = "1", description = "Server address, e.g. 192.168.1.100:12200")
        private String address;

        @Option(names = "--desc", description = "Description for this server.", defaultValue = "")
        private String desc;

        @Option(names = "--json", description = "Output in JSON format.")
        private boolean json;

        @Override
        public Integer call() {
            if (!ServerStore.isValidAddress(address)) {
                if (json) {
                    Map<String, Object> out = new LinkedHashMap<>();
                    out.put("success", false);
                    out.put("error", "地址格式错误，应为 host:port");
                    JsonPrinter.print(out);
                } else {
                    System.err.println("❌ 地址格式错误，应为 host:port，例如 192.168.1.100:12200");
                }
                return ExitCodes.BAD_ARGS;
            }
            try {
                new ServerStore().add(name, address, desc);
                if (json) {
                    Map<String, Object> out = new LinkedHashMap<>();
                    out.put("success", true);
                    out.put("name", name);
                    out.put("address", address);
                    JsonPrinter.print(out);
                } else {
                    System.out.println("✅ 已保存 " + name + " -> " + address);
                }
                return ExitCodes.SUCCESS;
            } catch (Exception e) {
                if (json) {
                    Map<String, Object> out = new LinkedHashMap<>();
                    out.put("success", false);
                    out.put("error", e.getMessage());
                    JsonPrinter.print(out);
                } else {
                    System.err.println("❌ 保存失败: " + e.getMessage());
                }
                return ExitCodes.BAD_ARGS;
            }
        }
    }

    @Command(name = "list", mixinStandardHelpOptions = true,
        description = "List server aliases, optionally filtered by keyword.")
    static class List implements Callable<Integer> {

        @Parameters(index = "0", description = "Optional keyword filter.", defaultValue = "")
        private String keyword;

        @Option(names = "--json", description = "Output in JSON format.")
        private boolean json;

        @Override
        public Integer call() {
            Map<String, ServerEntry> servers;
            try {
                servers = new ServerStore().loadAll();
            } catch (Exception e) {
                OutputFormatter.printError(
                    "读取服务配置失败: " + e.getMessage(), ExitCodes.BAD_ARGS, json);
                return ExitCodes.BAD_ARGS;
            }

            java.util.List<Map<String, Object>> list = new ArrayList<>();
            for (Map.Entry<String, ServerEntry> entry : servers.entrySet()) {
                String name = entry.getKey();
                if (!keyword.isEmpty() && !name.contains(keyword)) {
                    continue;
                }
                ServerEntry server = entry.getValue();
                Map<String, Object> item = new LinkedHashMap<>();
                item.put("name", name);
                item.put("address", server.getAddress());
                item.put("desc", server.getDesc() != null ? server.getDesc() : "");
                list.add(item);
            }

            if (json) {
                JsonPrinter.print(list);
                return ExitCodes.SUCCESS;
            }

            if (list.isEmpty()) {
                if (servers.isEmpty()) {
                    System.out.println("No servers configured. Use 'sofarpc server add' to add one.");
                } else {
                    System.out.println("No servers matched keyword: " + keyword);
                }
                return ExitCodes.SUCCESS;
            }

            System.out.printf("%-20s %-30s %s%n", "NAME", "ADDRESS", "DESC");
            for (Map<String, Object> item : list) {
                System.out.printf("%-20s %-30s %s%n",
                    item.get("name"), item.get("address"), item.get("desc"));
            }
            return ExitCodes.SUCCESS;
        }
    }

    @Command(name = "remove", mixinStandardHelpOptions = true,
        description = "Remove a server alias.")
    static class Remove implements Callable<Integer> {

        @Parameters(index = "0", description = "Server alias name to remove.")
        private String name;

        @Option(names = "--json", description = "Output in JSON format.")
        private boolean json;

        @Override
        public Integer call() {
            try {
                boolean removed = new ServerStore().remove(name);
                if (json) {
                    Map<String, Object> out = new LinkedHashMap<>();
                    out.put("success", removed);
                    out.put("name", name);
                    if (!removed) {
                        out.put("error", "别名不存在");
                    }
                    JsonPrinter.print(out);
                } else if (removed) {
                    System.out.println("✅ 已删除 " + name);
                } else {
                    System.err.println("❌ 别名不存在: " + name);
                }
                return removed ? ExitCodes.SUCCESS : ExitCodes.ALIAS_NOT_FOUND;
            } catch (Exception e) {
                if (json) {
                    Map<String, Object> out = new LinkedHashMap<>();
                    out.put("success", false);
                    out.put("error", e.getMessage());
                    JsonPrinter.print(out);
                } else {
                    System.err.println("❌ 删除失败: " + e.getMessage());
                }
                return ExitCodes.BAD_ARGS;
            }
        }
    }

    @Command(name = "export", mixinStandardHelpOptions = true,
        description = "Export server list as YAML to stdout.")
    static class Export implements Callable<Integer> {

        @Override
        public Integer call() {
            try {
                System.out.print(new ServerStore().exportYaml());
                return ExitCodes.SUCCESS;
            } catch (Exception e) {
                System.err.println("Export failed: " + e.getMessage());
                return ExitCodes.BAD_ARGS;
            }
        }
    }

    @Command(name = "import", mixinStandardHelpOptions = true,
        description = "Import server list from a YAML file.")
    static class Import implements Callable<Integer> {

        @Parameters(index = "0", description = "Path to servers.yaml file.")
        private File file;

        @Override
        public Integer call() {
            if (!file.exists()) {
                System.err.println("❌ 文件不存在: " + file.getPath());
                return ExitCodes.BAD_ARGS;
            }
            try {
                ServerStore store = new ServerStore();
                java.util.List<String> skipped = new ArrayList<>();
                java.util.List<String> warnings = new ArrayList<>();
                int count = store.importFromFile(file, skipped, warnings);
                for (String w : warnings) {
                    System.err.println("⚠️  " + w);
                }
                for (String msg : skipped) {
                    System.err.println("⚠️  跳过: " + msg);
                }
                System.out.println("✅ 已导入 " + count + " 个服务");
                return ExitCodes.SUCCESS;
            } catch (Exception e) {
                System.err.println("Import failed: " + e.getMessage());
                return ExitCodes.BAD_ARGS;
            }
        }
    }
}
