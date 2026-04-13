package com.sofarpc.cli.core;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.dataformat.yaml.YAMLFactory;
import com.fasterxml.jackson.dataformat.yaml.YAMLGenerator;

import java.io.File;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.StandardCopyOption;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.TreeMap;

/**
 * Manages server aliases stored in ~/.sofarpc/servers.yaml.
 *
 * @author wuweihua
 */
public class ServerStore {

    private static final String SERVERS_FILE = GlobalConfig.getConfigDir() + File.separator + "servers.yaml";

    private final ObjectMapper mapper;

    public ServerStore() {
        YAMLFactory factory = new YAMLFactory();
        factory.disable(YAMLGenerator.Feature.WRITE_DOC_START_MARKER);
        this.mapper = new ObjectMapper(factory);
    }

    /**
     * Load all servers from yaml file.
     *
     * @throws IOException if the file exists but cannot be read or parsed
     */
    @SuppressWarnings("unchecked")
    public Map<String, ServerEntry> loadAll() throws IOException {
        File file = new File(SERVERS_FILE);
        if (!file.exists()) {
            return new TreeMap<>();
        }
        Map<String, Object> root = mapper.readValue(file, LinkedHashMap.class);
        return parseServersRoot(root, "servers.yaml");
    }

    @SuppressWarnings("unchecked")
    private static Map<String, ServerEntry> parseServersRoot(Map<String, Object> root, String source)
        throws IOException {
        if (root == null) {
            throw new IOException(source + " 内容为空或根节点为 null，疑似损坏");
        }
        if (!root.containsKey("servers")) {
            throw new IOException(source + " 缺少顶层字段 'servers'");
        }
        Object serversObj = root.get("servers");
        if (serversObj == null) {
            throw new IOException(source + " 顶层字段 'servers' 为 null；如需清空请写 'servers: {}'");
        }
        if (!(serversObj instanceof Map)) {
            throw new IOException(source + " 顶层字段 'servers' 必须是 map，实际类型: "
                + serversObj.getClass().getSimpleName());
        }
        Map<String, Object> serversMap = (Map<String, Object>) serversObj;
        Map<String, ServerEntry> result = new TreeMap<>();
        for (Map.Entry<String, Object> entry : serversMap.entrySet()) {
            if (!(entry.getValue() instanceof Map)) {
                throw new IOException(source + " 别名 '" + entry.getKey()
                    + "' 必须是 map，实际类型: "
                    + (entry.getValue() == null ? "null" : entry.getValue().getClass().getSimpleName()));
            }
            Map<String, Object> val = (Map<String, Object>) entry.getValue();
            String address = readStringField(entry.getKey(), val, "address", true);
            String desc = readStringField(entry.getKey(), val, "desc", false);
            result.put(entry.getKey(), new ServerEntry(address, desc == null ? "" : desc));
        }
        return result;
    }

    private static String readStringField(String alias, Map<String, Object> val,
                                          String field, boolean required) throws IOException {
        Object raw = val.get(field);
        if (raw == null) {
            if (required) {
                throw new IOException("servers.yaml 别名 '" + alias + "' 缺少字段: " + field);
            }
            return null;
        }
        if (!(raw instanceof String)) {
            throw new IOException("servers.yaml 别名 '" + alias + "' 的 " + field
                + " 必须为字符串，实际类型: " + raw.getClass().getSimpleName());
        }
        return (String) raw;
    }

    /**
     * Save all servers to yaml file using atomic write (temp file + rename).
     *
     * @throws IOException if writing fails
     */
    public void saveAll(Map<String, ServerEntry> servers) throws IOException {
        File file = new File(SERVERS_FILE);
        file.getParentFile().mkdirs();
        Map<String, Object> root = new LinkedHashMap<>();
        Map<String, Object> serversMap = new LinkedHashMap<>();
        for (Map.Entry<String, ServerEntry> entry : servers.entrySet()) {
            Map<String, Object> val = new LinkedHashMap<>();
            val.put("address", entry.getValue().getAddress());
            if (entry.getValue().getDesc() != null && !entry.getValue().getDesc().isEmpty()) {
                val.put("desc", entry.getValue().getDesc());
            }
            serversMap.put(entry.getKey(), val);
        }
        root.put("servers", serversMap);
        File tmpFile = new File(SERVERS_FILE + ".tmp");
        mapper.writeValue(tmpFile, root);
        Files.move(tmpFile.toPath(), file.toPath(), StandardCopyOption.ATOMIC_MOVE);
    }

    /**
     * Add or update a server entry.
     */
    public void add(String name, String address, String desc) throws IOException {
        Map<String, ServerEntry> servers = loadAll();
        servers.put(name, new ServerEntry(address, desc));
        saveAll(servers);
    }

    /**
     * Remove a server entry. Returns true if the entry existed.
     */
    public boolean remove(String name) throws IOException {
        Map<String, ServerEntry> servers = loadAll();
        if (servers.remove(name) != null) {
            saveAll(servers);
            return true;
        }
        return false;
    }

    /**
     * Resolve a server alias to its address. Returns null if not found.
     */
    public String resolveAddress(String name) throws IOException {
        Map<String, ServerEntry> servers = loadAll();
        ServerEntry entry = servers.get(name);
        return entry != null ? entry.getAddress() : null;
    }

    /**
     * Export servers map as YAML string to stdout.
     */
    public String exportYaml() throws IOException {
        Map<String, ServerEntry> servers = loadAll();
        Map<String, Object> root = new LinkedHashMap<>();
        Map<String, Object> serversMap = new LinkedHashMap<>();
        for (Map.Entry<String, ServerEntry> entry : servers.entrySet()) {
            Map<String, Object> val = new LinkedHashMap<>();
            val.put("address", entry.getValue().getAddress());
            if (entry.getValue().getDesc() != null && !entry.getValue().getDesc().isEmpty()) {
                val.put("desc", entry.getValue().getDesc());
            }
            serversMap.put(entry.getKey(), val);
        }
        root.put("servers", serversMap);
        return mapper.writeValueAsString(root);
    }

    /**
     * Import servers from a yaml file. Merges into existing entries.
     * Entries with invalid address format are skipped and reported via the skipped list.
     * If the local servers.yaml is structurally corrupted, it will be backed up and
     * replaced — this keeps `import` usable as a self-recovery path.
     *
     * @param importFile the YAML file to import
     * @param skipped    list to collect per-entry skip messages (may be null)
     * @param warnings   list to collect structural warnings such as corrupted-local recovery (may be null)
     * @return number of successfully imported entries
     */
    @SuppressWarnings("unchecked")
    public int importFromFile(File importFile, java.util.List<String> skipped,
                              java.util.List<String> warnings) throws IOException {
        Map<String, Object> root = mapper.readValue(importFile, LinkedHashMap.class);
        String source = importFile.getName();
        Map<String, ServerEntry> imported = parseServersRoot(root, source);
        Map<String, ServerEntry> existing;
        try {
            existing = loadAll();
        } catch (IOException e) {
            existing = new TreeMap<>();
            File original = new File(SERVERS_FILE);
            if (original.exists()) {
                File backup = new File(SERVERS_FILE + ".corrupted." + System.currentTimeMillis());
                Files.copy(original.toPath(), backup.toPath());
                if (warnings != null) {
                    warnings.add("现有配置损坏，已备份到 " + backup.getAbsolutePath()
                        + " 并将被覆盖: " + e.getMessage());
                }
            } else if (warnings != null) {
                warnings.add("现有配置损坏将被覆盖: " + e.getMessage());
            }
        }
        int count = 0;
        for (Map.Entry<String, ServerEntry> entry : imported.entrySet()) {
            String address = entry.getValue().getAddress();
            if (!isValidAddress(address)) {
                if (skipped != null) {
                    skipped.add(entry.getKey() + " (无效地址: " + address + ")");
                }
                continue;
            }
            existing.put(entry.getKey(), entry.getValue());
            count++;
        }
        saveAll(existing);
        return count;
    }

    /**
     * Validate address format: must be host:port with port in 1-65535.
     */
    public static boolean isValidAddress(String addr) {
        if (addr == null) {
            return false;
        }
        int colonIdx = addr.lastIndexOf(':');
        if (colonIdx <= 0 || colonIdx == addr.length() - 1) {
            return false;
        }
        String host = addr.substring(0, colonIdx);
        String portStr = addr.substring(colonIdx + 1);
        if (host.isEmpty()) {
            return false;
        }
        try {
            int port = Integer.parseInt(portStr);
            return port > 0 && port <= 65535;
        } catch (NumberFormatException e) {
            return false;
        }
    }

    /**
     * A server entry with address and description.
     */
    public static class ServerEntry {
        private final String address;
        private final String desc;

        public ServerEntry(String address, String desc) {
            this.address = address;
            this.desc = desc;
        }

        public String getAddress() {
            return address;
        }

        public String getDesc() {
            return desc;
        }
    }
}
