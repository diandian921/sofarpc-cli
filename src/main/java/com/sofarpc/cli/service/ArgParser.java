package com.sofarpc.cli.service;

import com.sofarpc.cli.core.JacksonHolder;

import java.util.List;

/**
 * Parse CLI argument types and argument values into typed arrays.
 *
 * @author wuwh
 */
public final class ArgParser {

    private ArgParser() {
    }

    /**
     * Parse arg-types and args strings into structured arrays.
     *
     * @param argTypes comma-separated type names, may be null
     * @param args     JSON string (object or array), may be null
     * @return parsed types and values
     * @throws IllegalArgumentException if validation fails
     */
    public static ParsedArgs parse(String argTypes, String args) {
        if (args == null || args.isEmpty()) {
            return new ParsedArgs(new String[0], new Object[0]);
        }
        if (argTypes == null || argTypes.isEmpty()) {
            throw new IllegalArgumentException("--arg-types is required when args is specified");
        }

        String[] types = splitTypes(argTypes);
        Object[] values = parseValues(args, types.length);
        return new ParsedArgs(types, values);
    }

    private static String[] splitTypes(String argTypes) {
        String[] types = argTypes.split(",");
        for (int i = 0; i < types.length; i++) {
            types[i] = types[i].trim();
        }
        return types;
    }

    @SuppressWarnings("unchecked")
    private static Object[] parseValues(String args, int typeCount) {
        try {
            String trimmed = args.trim();
            if (trimmed.startsWith("[")) {
                List<Object> list = JacksonHolder.MAPPER.readValue(trimmed,
                    JacksonHolder.MAPPER.getTypeFactory().constructCollectionType(List.class, Object.class));
                if (list.size() != typeCount) {
                    throw new IllegalArgumentException(
                        "args array size (" + list.size() + ") does not match arg-types count (" + typeCount + ")");
                }
                return list.toArray();
            }
            if (typeCount != 1) {
                throw new IllegalArgumentException(
                    "Single object arg provided but arg-types has " + typeCount + " types");
            }
            return new Object[]{JacksonHolder.MAPPER.readValue(trimmed, Object.class)};
        } catch (IllegalArgumentException e) {
            throw e;
        } catch (Exception e) {
            throw new IllegalArgumentException("Failed to parse args JSON: " + e.getMessage(), e);
        }
    }

    /**
     * Parsed argument types and values.
     */
    public static class ParsedArgs {
        private final String[] types;
        private final Object[] values;

        public ParsedArgs(String[] types, Object[] values) {
            this.types = types;
            this.values = values;
        }

        public String[] getTypes() {
            return types;
        }

        public Object[] getValues() {
            return values;
        }
    }
}
