package com.sofarpc.cli.command;

import com.sofarpc.cli.core.ServerStore;
import com.sofarpc.cli.core.ServerStore.ServerEntry;
import picocli.CommandLine.Command;
import picocli.CommandLine.Option;
import picocli.CommandLine.Parameters;

import java.io.File;
import java.util.Map;

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
public class ServerCommand implements Runnable {

    @Override
    public void run() {
        new picocli.CommandLine(this).usage(System.out);
    }

    @Command(name = "add", description = "Add or update a server alias.")
    static class Add implements Runnable {

        @Parameters(index = "0", description = "Server alias name.")
        private String name;

        @Parameters(index = "1", description = "Server address, e.g. 192.168.1.100:12200")
        private String address;

        @Option(names = "--desc", description = "Description for this server.", defaultValue = "")
        private String desc;

        @Override
        public void run() {
            new ServerStore().add(name, address, desc);
            System.out.println("✅ 已保存 " + name + " -> " + address);
        }
    }

    @Command(name = "list", description = "List server aliases, optionally filtered by keyword.")
    static class List implements Runnable {

        @Parameters(index = "0", description = "Optional keyword filter.", defaultValue = "")
        private String keyword;

        @Override
        public void run() {
            Map<String, ServerEntry> servers = new ServerStore().loadAll();
            if (servers.isEmpty()) {
                System.out.println("No servers configured. Use 'sofarpc server add' to add one.");
                return;
            }

            System.out.printf("%-20s %-30s %s%n", "NAME", "ADDRESS", "DESC");
            for (Map.Entry<String, ServerEntry> entry : servers.entrySet()) {
                String name = entry.getKey();
                if (!keyword.isEmpty() && !name.contains(keyword)) {
                    continue;
                }
                ServerEntry server = entry.getValue();
                System.out.printf("%-20s %-30s %s%n",
                    name,
                    server.getAddress(),
                    server.getDesc() != null ? server.getDesc() : "");
            }
        }
    }

    @Command(name = "remove", description = "Remove a server alias.")
    static class Remove implements Runnable {

        @Parameters(index = "0", description = "Server alias name to remove.")
        private String name;

        @Override
        public void run() {
            boolean removed = new ServerStore().remove(name);
            if (removed) {
                System.out.println("✅ 已删除 " + name);
            } else {
                System.err.println("❌ 别名不存在: " + name);
            }
        }
    }

    @Command(name = "export", description = "Export server list as YAML to stdout.")
    static class Export implements Runnable {

        @Override
        public void run() {
            try {
                System.out.print(new ServerStore().exportYaml());
            } catch (Exception e) {
                System.err.println("Export failed: " + e.getMessage());
            }
        }
    }

    @Command(name = "import", description = "Import server list from a YAML file.")
    static class Import implements Runnable {

        @Parameters(index = "0", description = "Path to servers.yaml file.")
        private File file;

        @Override
        public void run() {
            if (!file.exists()) {
                System.err.println("❌ 文件不存在: " + file.getPath());
                return;
            }
            try {
                int count = new ServerStore().importFromFile(file);
                System.out.println("✅ 已导入 " + count + " 个服务");
            } catch (Exception e) {
                System.err.println("Import failed: " + e.getMessage());
            }
        }
    }
}
