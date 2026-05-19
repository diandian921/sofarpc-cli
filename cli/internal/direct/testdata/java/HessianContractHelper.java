import com.caucho.hessian.io.Hessian2Input;
import com.caucho.hessian.io.Hessian2Output;
import com.caucho.hessian.io.SerializerFactory;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.Serializable;
import java.math.BigDecimal;
import java.math.BigInteger;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Date;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

public final class HessianContractHelper {
    public enum Status {
        ACTIVE,
        INACTIVE
    }

    public static final class QueryRequest implements Serializable {
        public Long mpCode;
        public Double ratio;
        public String emoji;

        public QueryRequest() {
        }

        public QueryRequest(Long mpCode, Double ratio, String emoji) {
            this.mpCode = mpCode;
            this.ratio = ratio;
            this.emoji = emoji;
        }
    }

    public static final class QueryResponse implements Serializable {
        public Boolean success;
        public BigDecimal amount;
        public List<String> tags;

        public QueryResponse() {
        }

        public QueryResponse(Boolean success, BigDecimal amount, List<String> tags) {
            this.success = success;
            this.amount = amount;
            this.tags = tags;
        }
    }

    public static final class EnumRequest implements Serializable {
        public Status status;

        public EnumRequest() {
        }

        public EnumRequest(Status status) {
            this.status = status;
        }
    }

    public static final class EnumResponse implements Serializable {
        public Status status;
        public List<Status> history;

        public EnumResponse() {
        }

        public EnumResponse(Status status, List<Status> history) {
            this.status = status;
            this.history = history;
        }
    }

    public static final class ComplexResponse implements Serializable {
        public QueryResponse primary;
        public List<QueryResponse> history;
        public Map<String, Object> attributes;
        public List<Object> mixed;

        public ComplexResponse() {
        }

        public ComplexResponse(QueryResponse primary, List<QueryResponse> history,
                Map<String, Object> attributes, List<Object> mixed) {
            this.primary = primary;
            this.history = history;
            this.attributes = attributes;
            this.mixed = mixed;
        }
    }

    public static void main(String[] args) throws Exception {
        if (args.length < 1) {
            throw new IllegalArgumentException("missing mode");
        }
        switch (args[0]) {
            case "decode-any":
                requireArgs(args, 2);
                System.out.println(describe(readAny(fromHex(args[1]))));
                return;
            case "decode-query-request":
                requireArgs(args, 2);
                System.out.println(describe(readAs(fromHex(args[1]), QueryRequest.class)));
                return;
            case "decode-complex-response":
                requireArgs(args, 2);
                System.out.println(describe(readAs(fromHex(args[1]), ComplexResponse.class)));
                return;
            case "decode-status":
                requireArgs(args, 2);
                System.out.println(describe(readAs(fromHex(args[1]), Status.class)));
                return;
            case "decode-enum-request":
                requireArgs(args, 2);
                System.out.println(describe(readAs(fromHex(args[1]), EnumRequest.class)));
                return;
            case "encode":
                requireArgs(args, 2);
                System.out.println(toHex(encode(caseValue(args[1]))));
                return;
            default:
                throw new IllegalArgumentException("unknown mode: " + args[0]);
        }
    }

    private static Object readAny(byte[] data) throws Exception {
        Hessian2Input in = new Hessian2Input(new ByteArrayInputStream(data));
        in.setSerializerFactory(serializerFactory());
        return in.readObject();
    }

    private static Object readAs(byte[] data, Class<?> type) throws Exception {
        Hessian2Input in = new Hessian2Input(new ByteArrayInputStream(data));
        in.setSerializerFactory(serializerFactory());
        return in.readObject(type);
    }

    private static byte[] encode(Object value) throws Exception {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        Hessian2Output out = new Hessian2Output(bytes);
        out.setSerializerFactory(serializerFactory());
        out.writeObject(value);
        out.flush();
        return bytes.toByteArray();
    }

    private static SerializerFactory serializerFactory() {
        SerializerFactory factory = new SerializerFactory();
        factory.setAllowNonSerializable(true);
        return factory;
    }

    private static Object caseValue(String name) {
        switch (name) {
            case "string-emoji":
                return "a🙂b";
            case "long":
                return Long.valueOf(433905635109773312L);
            case "integer":
                return Integer.valueOf(5);
            case "double-whole":
                return Double.valueOf(2.0d);
            case "big-decimal":
                return new BigDecimal("1000.50");
            case "big-integer":
                return new BigInteger("9223372036854775807");
            case "list-with-null":
                return new ArrayList<Object>(Arrays.asList(null, Long.valueOf(1L), "two"));
            case "map-long-key":
                LinkedHashMap<Object, Object> map = new LinkedHashMap<Object, Object>();
                map.put(Long.valueOf(7L), "seven");
                map.put("name", "alice");
                return map;
            case "query-response":
                return new QueryResponse(Boolean.TRUE, new BigDecimal("113795.2485"), Arrays.asList("A", "B"));
            case "nested-response":
                LinkedHashMap<String, Object> attrs = new LinkedHashMap<String, Object>();
                attrs.put("mpCode", Long.valueOf(433905635109773312L));
                attrs.put("nullable", null);
                attrs.put("ratio", Double.valueOf(2.0d));
                return new ComplexResponse(
                        new QueryResponse(Boolean.TRUE, new BigDecimal("1.23"), Arrays.asList("P")),
                        Arrays.asList(new QueryResponse(Boolean.FALSE, new BigDecimal("0.00"), Arrays.asList("H"))),
                        attrs,
                        new ArrayList<Object>(Arrays.asList(null, "x", Long.valueOf(9L))));
            case "date":
                return new Date(0L);
            case "bytes":
                return new byte[] {0x01, 0x02, (byte) 0xff};
            case "enum":
                return Status.ACTIVE;
            case "enum-response":
                return new EnumResponse(Status.ACTIVE, Arrays.asList(Status.INACTIVE));
            default:
                throw new IllegalArgumentException("unknown case: " + name);
        }
    }

    private static String describe(Object value) {
        if (value == null) {
            return "null";
        }
        if (value instanceof QueryRequest) {
            QueryRequest v = (QueryRequest) value;
            return "QueryRequest{mpCode=" + describe(v.mpCode)
                    + ",ratio=" + describe(v.ratio)
                    + ",emoji=" + describe(v.emoji)
                    + "}";
        }
        if (value instanceof QueryResponse) {
            QueryResponse v = (QueryResponse) value;
            return "QueryResponse{success=" + describe(v.success)
                    + ",amount=" + describe(v.amount)
                    + ",tags=" + describe(v.tags)
                    + "}";
        }
        if (value instanceof ComplexResponse) {
            ComplexResponse v = (ComplexResponse) value;
            return "ComplexResponse{primary=" + describe(v.primary)
                    + ",history=" + describe(v.history)
                    + ",attributes=" + describe(v.attributes)
                    + ",mixed=" + describe(v.mixed)
                    + "}";
        }
        if (value instanceof EnumRequest) {
            EnumRequest v = (EnumRequest) value;
            return "EnumRequest{status=" + describe(v.status) + "}";
        }
        if (value instanceof EnumResponse) {
            EnumResponse v = (EnumResponse) value;
            return "EnumResponse{status=" + describe(v.status)
                    + ",history=" + describe(v.history)
                    + "}";
        }
        if (value instanceof Status) {
            return "Status:" + ((Status) value).name();
        }
        if (value instanceof byte[]) {
            return "byte[]:" + toHex((byte[]) value);
        }
        if (value instanceof Date) {
            return "Date:" + ((Date) value).getTime();
        }
        if (value instanceof List) {
            StringBuilder out = new StringBuilder("List[");
            boolean first = true;
            for (Object item : (List<?>) value) {
                if (!first) {
                    out.append(",");
                }
                out.append(describe(item));
                first = false;
            }
            return out.append("]").toString();
        }
        if (value instanceof Map) {
            StringBuilder out = new StringBuilder("Map{");
            boolean first = true;
            for (Map.Entry<?, ?> entry : ((Map<?, ?>) value).entrySet()) {
                if (!first) {
                    out.append(",");
                }
                out.append(describe(entry.getKey())).append("=").append(describe(entry.getValue()));
                first = false;
            }
            return out.append("}").toString();
        }
        return value.getClass().getName() + ":" + String.valueOf(value);
    }

    private static void requireArgs(String[] args, int n) {
        if (args.length != n) {
            throw new IllegalArgumentException("wrong arg count");
        }
    }

    private static byte[] fromHex(String text) {
        if ((text.length() & 1) != 0) {
            throw new IllegalArgumentException("odd hex length");
        }
        byte[] out = new byte[text.length() / 2];
        for (int i = 0; i < out.length; i++) {
            int hi = Character.digit(text.charAt(i * 2), 16);
            int lo = Character.digit(text.charAt(i * 2 + 1), 16);
            if (hi < 0 || lo < 0) {
                throw new IllegalArgumentException("invalid hex");
            }
            out[i] = (byte) ((hi << 4) | lo);
        }
        return out;
    }

    private static String toHex(byte[] bytes) {
        char[] table = "0123456789abcdef".toCharArray();
        char[] out = new char[bytes.length * 2];
        for (int i = 0; i < bytes.length; i++) {
            int b = bytes[i] & 0xff;
            out[i * 2] = table[b >>> 4];
            out[i * 2 + 1] = table[b & 0x0f];
        }
        return new String(out);
    }
}
