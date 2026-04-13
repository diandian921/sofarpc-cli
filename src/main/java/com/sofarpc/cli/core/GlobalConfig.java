package com.sofarpc.cli.core;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.dataformat.yaml.YAMLFactory;

import java.io.File;
import java.io.IOException;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Global configuration reader for ~/.sofarpc/config.yaml.
 * Priority: command line args > config.yaml > built-in defaults.
 *
 * @author wuweihua
 */
public class GlobalConfig {

    private static final String CONFIG_DIR = System.getProperty("user.home") + File.separator + ".sofarpc";
    private static final String CONFIG_FILE = CONFIG_DIR + File.separator + "config.yaml";

    private static final int DEFAULT_TIMEOUT = 5000;
    private static final int DEFAULT_PARALLEL = 1;

    private static GlobalConfig instance;

    private int timeout = DEFAULT_TIMEOUT;
    private int parallel = DEFAULT_PARALLEL;

    private GlobalConfig() {
    }

    @SuppressWarnings("unchecked")
    public static synchronized GlobalConfig getInstance() {
        if (instance == null) {
            instance = new GlobalConfig();
            File file = new File(CONFIG_FILE);
            if (file.exists()) {
                try {
                    ObjectMapper mapper = new ObjectMapper(new YAMLFactory());
                    Map<String, Object> root = mapper.readValue(file, LinkedHashMap.class);
                    Object defaults = root.get("defaults");
                    if (defaults instanceof Map) {
                        Map<String, Object> defaultsMap = (Map<String, Object>) defaults;
                        instance.timeout = readPositiveInt(defaultsMap, "timeout", DEFAULT_TIMEOUT);
                        instance.parallel = readPositiveInt(defaultsMap, "parallel", DEFAULT_PARALLEL);
                    }
                } catch (IOException e) {
                    System.err.println("Warning: failed to read config.yaml, using defaults. " + e.getMessage());
                } catch (Exception e) {
                    System.err.println("Warning: invalid config.yaml, using defaults. " + e.getMessage());
                }
            }
        }
        return instance;
    }

    private static int readPositiveInt(Map<String, Object> defaultsMap, String key, int fallback) {
        if (!defaultsMap.containsKey(key)) {
            return fallback;
        }
        Object raw = defaultsMap.get(key);
        if (!(raw instanceof Number)) {
            System.err.println("Warning: config.yaml defaults." + key
                + " must be a number, using default: " + fallback);
            return fallback;
        }
        int value = ((Number) raw).intValue();
        if (value <= 0) {
            System.err.println("Warning: config.yaml defaults." + key
                + " must be positive, using default: " + fallback);
            return fallback;
        }
        return value;
    }

    public int getTimeout() {
        return timeout;
    }

    public int getParallel() {
        return parallel;
    }

    public static String getConfigDir() {
        return CONFIG_DIR;
    }
}
