package com.sofarpc.cli.core;

import com.alipay.sofa.rpc.api.GenericService;
import com.alipay.sofa.rpc.config.ConsumerConfig;
import com.fasterxml.jackson.databind.ObjectMapper;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Manages GenericService instances with connection caching.
 * Cache key: "serverAlias::interfaceId"
 *
 * @author wuweihua
 */
public class RpcClientFactory {

    private static final Map<String, GenericService> CACHE = new ConcurrentHashMap<>();
    private static final Map<String, ConsumerConfig<GenericService>> CONFIG_CACHE = new ConcurrentHashMap<>();

    static {
        Runtime.getRuntime().addShutdownHook(new Thread(RpcClientFactory::destroyAll));
    }

    /**
     * Get or create a GenericService instance for the given server address and interface.
     *
     * @param serverAlias server alias (for cache key)
     * @param address     bolt address, e.g. "192.168.1.100:12200"
     * @param interfaceId full qualified interface name
     * @param timeout     timeout in milliseconds
     * @return GenericService instance
     */
    public static GenericService getOrCreate(String serverAlias, String address, String interfaceId, int timeout) {
        String cacheKey = serverAlias + "::" + interfaceId;
        return CACHE.computeIfAbsent(cacheKey, key -> {
            ConsumerConfig<GenericService> config = new ConsumerConfig<GenericService>()
                .setInterfaceId(interfaceId)
                .setGeneric(true)
                .setProtocol("bolt")
                .setDirectUrl("bolt://" + address)
                .setRegister(false)
                .setSubscribe(false)
                .setTimeout(timeout)
                .setConnectTimeout(timeout)
                .setConnectionNum(1)
                .setLazy(false)
                .setReconnectPeriod(0);
            CONFIG_CACHE.put(cacheKey, config);
            return config.refer();
        });
    }

    private static final ObjectMapper OBJECT_MAPPER = new ObjectMapper();

    /**
     * Flatten the generic invocation result by:
     * 1. Serializing GenericObject to JSON (Jackson handles the getters)
     * 2. Parsing back to Map
     * 3. Recursively unwrapping type/fields/fieldNames wrappers
     */
    public static Object flattenResult(Object obj) {
        if (obj == null) {
            return null;
        }
        try {
            // Serialize GenericObject -> JSON string -> Map, then unwrap
            String json = OBJECT_MAPPER.writeValueAsString(obj);
            Object parsed = OBJECT_MAPPER.readValue(json, Object.class);
            return unwrap(parsed);
        } catch (Exception e) {
            return obj;
        }
    }

    @SuppressWarnings("unchecked")
    private static Object unwrap(Object obj) {
        if (obj == null) {
            return null;
        }
        if (obj instanceof Map) {
            Map<String, Object> map = (Map<String, Object>) obj;
            // GenericObject wrapper: has "type" + "fields" keys
            if (map.containsKey("fields") && map.containsKey("type")) {
                Object fieldsObj = map.get("fields");
                if (fieldsObj instanceof Map) {
                    Map<String, Object> fields = (Map<String, Object>) fieldsObj;
                    Map<String, Object> flattened = new LinkedHashMap<>();
                    for (Map.Entry<String, Object> entry : fields.entrySet()) {
                        flattened.put(entry.getKey(), unwrap(entry.getValue()));
                    }
                    return flattened;
                }
            }
            // Regular map, recurse but skip metadata keys
            Map<String, Object> result = new LinkedHashMap<>();
            for (Map.Entry<String, Object> entry : map.entrySet()) {
                if ("fieldNames".equals(entry.getKey())) {
                    continue;
                }
                result.put(entry.getKey(), unwrap(entry.getValue()));
            }
            return result;
        }
        if (obj instanceof List) {
            List<Object> result = new ArrayList<>();
            for (Object item : (List<?>) obj) {
                result.add(unwrap(item));
            }
            return result;
        }
        return obj;
    }

    /**
     * Release all cached connections.
     */
    public static void destroyAll() {
        for (Map.Entry<String, ConsumerConfig<GenericService>> entry : CONFIG_CACHE.entrySet()) {
            try {
                entry.getValue().unRefer();
            } catch (Exception e) {
                // Ignore errors during shutdown
            }
        }
        CACHE.clear();
        CONFIG_CACHE.clear();
    }
}
