package com.sofarpc.daemon.rpc;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.sofarpc.daemon.protocol.JsonCodec;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Unwraps SOFARPC GenericObject responses into plain JSON-compatible structures.
 *
 * @author wuwh
 */
public final class ResultFlattener {

    private static final String GENERIC_TYPE_KEY = "type";
    private static final String GENERIC_FIELDS_KEY = "fields";
    private static final String GENERIC_FIELD_NAMES_KEY = "fieldNames";

    private ResultFlattener() {
    }

    public static Object flatten(Object obj) {
        if (obj == null) {
            return null;
        }
        ObjectMapper mapper = JsonCodec.mapper();
        try {
            String json = mapper.writeValueAsString(obj);
            Object parsed = mapper.readValue(json, Object.class);
            return unwrap(parsed);
        } catch (Exception e) {
            return obj;
        }
    }

    @SuppressWarnings("unchecked")
    private static Object unwrap(Object obj) {
        if (obj instanceof Map) {
            return unwrapMap((Map<String, Object>) obj);
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

    @SuppressWarnings("unchecked")
    private static Object unwrapMap(Map<String, Object> map) {
        if (map.containsKey(GENERIC_FIELDS_KEY) && map.containsKey(GENERIC_TYPE_KEY)) {
            Object fieldsObj = map.get(GENERIC_FIELDS_KEY);
            if (fieldsObj instanceof Map) {
                Map<String, Object> fields = (Map<String, Object>) fieldsObj;
                Map<String, Object> flattened = new LinkedHashMap<>();
                for (Map.Entry<String, Object> entry : fields.entrySet()) {
                    flattened.put(entry.getKey(), unwrap(entry.getValue()));
                }
                return flattened;
            }
        }
        Map<String, Object> result = new LinkedHashMap<>();
        for (Map.Entry<String, Object> entry : map.entrySet()) {
            if (GENERIC_FIELD_NAMES_KEY.equals(entry.getKey())) {
                continue;
            }
            result.put(entry.getKey(), unwrap(entry.getValue()));
        }
        return result;
    }
}
