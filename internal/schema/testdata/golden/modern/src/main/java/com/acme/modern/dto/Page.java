package com.acme.modern.dto;

import java.util.List;

public class Page<T> {
    private List<T> records;
    private int total;
}
