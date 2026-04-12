package com.sofarpc.cli.core;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.dataformat.yaml.YAMLFactory;
import com.fasterxml.jackson.dataformat.yaml.YAMLGenerator;

import java.io.File;
import java.io.IOException;
import java.util.Collections;
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
     */
    @SuppressWarnings("unchecked")
    public Map<String, ServerEntry> loadAll() {
        File file = new File(SERVERS_FILE);
        if (!file.exists()) {
            return new TreeMap<>();
        }
        try {
            Map<String, Object> root = mapper.readValue(file, LinkedHashMap.class);
            Object serversObj = root.get("servers");
            if (!(serversObj instanceof Map)) {
                return new TreeMap<>();
            }
            Map<String, Object> serversMap = (Map<String, Object>) serversObj;
            Map<String, ServerEntry> result = new TreeMap<>();
            for (Map.Entry<String, Object> entry : serversMap.entrySet()) {
                if (entry.getValue() instanceof Map) {
                    Map<String, Object> val = (Map<String, Object>) entry.getValue();
                    String address = (String) val.get("address");
                    String desc = (String) val.getOrDefault("desc", "");
                    result.put(entry.getKey(), new ServerEntry(address, desc));
                }
            }
            return result;
        } catch (IOException e) {
            System.err.println("Error reading servers.yaml: " + e.getMessage());
            return new TreeMap<>();
        }
    }

    /**
     * Save all servers to yaml file.
     */
    public void saveAll(Map<String, ServerEntry> servers) {
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
        try {
            mapper.writeValue(file, root);
        } catch (IOException e) {
            System.err.println("Error writing servers.yaml: " + e.getMessage());
        }
    }

    /**
     * Add or update a server entry.
     */
    public void add(String name, String address, String desc) {
        Map<String, ServerEntry> servers = loadAll();
        servers.put(name, new ServerEntry(address, desc));
        saveAll(servers);
    }

    /**
     * Remove a server entry. Returns true if the entry existed.
     */
    public boolean remove(String name) {
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
    public String resolveAddress(String name) {
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
     */
    @SuppressWarnings("unchecked")
    public int importFromFile(File importFile) throws IOException {
        Map<String, Object> root = mapper.readValue(importFile, LinkedHashMap.class);
        Object serversObj = root.get("servers");
        if (!(serversObj instanceof Map)) {
            return 0;
        }
        Map<String, Object> importedServers = (Map<String, Object>) serversObj;
        Map<String, ServerEntry> existing = loadAll();
        int count = 0;
        for (Map.Entry<String, Object> entry : importedServers.entrySet()) {
            if (entry.getValue() instanceof Map) {
                Map<String, Object> val = (Map<String, Object>) entry.getValue();
                String address = (String) val.get("address");
                String desc = (String) val.getOrDefault("desc", "");
                existing.put(entry.getKey(), new ServerEntry(address, desc));
                count++;
            }
        }
        saveAll(existing);
        return count;
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
