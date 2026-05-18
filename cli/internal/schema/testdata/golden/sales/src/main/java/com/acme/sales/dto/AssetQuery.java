package com.acme.sales.dto;

import java.util.List;
import java.util.Map;

public class AssetQuery {
    private Long mpCode;
    private List<String> tags;
    private Map<String, List<Long>> filters;
    private byte[] payload;
}
