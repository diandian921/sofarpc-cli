package com.sofarpc.daemon.rpc;

import com.alipay.hessian.generic.model.GenericObject;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Converts JSON-shaped arguments into SOFA Hessian generic model objects before
 * they enter {@link com.alipay.sofa.rpc.api.GenericService}.
 *
 * @author wuwh
 */
public final class GenericArgumentConverter {

    private static final String EXPLICIT_TYPE_KEY = "__type";
    private static final String EXPLICIT_AT_TYPE_KEY = "@type";

    private GenericArgumentConverter() {
    }

    public static Object[] convert(String[] argTypes, Object[] args) {
        if (argTypes == null || args == null) {
            return args;
        }
        Object[] converted = new Object[args.length];
        for (int i = 0; i < args.length; i++) {
            String argType = i < argTypes.length ? argTypes[i] : null;
            converted[i] = convertArgument(argType, args[i]);
        }
        return converted;
    }

    @SuppressWarnings("unchecked")
    private static Object convertArgument(String argType, Object value) {
        if (value instanceof Map && shouldWrapAsGenericObject(argType)) {
            return toGenericObject(argType, (Map<String, Object>) value);
        }
        return convertNested(value);
    }

    @SuppressWarnings("unchecked")
    private static Object convertNested(Object value) {
        if (value instanceof Map) {
            Map<String, Object> map = (Map<String, Object>) value;
            String explicitType = explicitType(map);
            if (explicitType != null) {
                return toGenericObject(explicitType, map);
            }
            Map<String, Object> copy = new LinkedHashMap<String, Object>();
            for (Map.Entry<String, Object> entry : map.entrySet()) {
                copy.put(entry.getKey(), convertNested(entry.getValue()));
            }
            return copy;
        }
        if (value instanceof List) {
            List<Object> copy = new ArrayList<Object>();
            for (Object item : (List<?>) value) {
                copy.add(convertNested(item));
            }
            return copy;
        }
        return value;
    }

    private static GenericObject toGenericObject(String type, Map<String, Object> fields) {
        GenericObject obj = new GenericObject(type);
        for (Map.Entry<String, Object> entry : fields.entrySet()) {
            String name = entry.getKey();
            if (EXPLICIT_TYPE_KEY.equals(name) || EXPLICIT_AT_TYPE_KEY.equals(name)) {
                continue;
            }
            obj.putField(name, convertNested(entry.getValue()));
        }
        return obj;
    }

    private static String explicitType(Map<String, Object> map) {
        Object v = map.get(EXPLICIT_TYPE_KEY);
        if (v instanceof String && !((String) v).isEmpty()) {
            return (String) v;
        }
        v = map.get(EXPLICIT_AT_TYPE_KEY);
        if (v instanceof String && !((String) v).isEmpty()) {
            return (String) v;
        }
        return null;
    }

    private static boolean shouldWrapAsGenericObject(String argType) {
        if (argType == null || argType.isEmpty()) {
            return false;
        }
        String type = eraseGeneric(argType);
        if (isPrimitive(type)) {
            return false;
        }
        if (type.startsWith("java.lang.") || type.startsWith("java.math.")) {
            return false;
        }
        if (type.startsWith("java.util.")) {
            return false;
        }
        return type.indexOf('.') >= 0;
    }

    private static String eraseGeneric(String type) {
        int idx = type.indexOf('<');
        if (idx >= 0) {
            return type.substring(0, idx).trim();
        }
        while (type.endsWith("[]")) {
            type = type.substring(0, type.length() - 2);
        }
        return type.trim();
    }

    private static boolean isPrimitive(String type) {
        return "boolean".equals(type)
                || "byte".equals(type)
                || "char".equals(type)
                || "short".equals(type)
                || "int".equals(type)
                || "long".equals(type)
                || "float".equals(type)
                || "double".equals(type)
                || "void".equals(type);
    }
}
