package com.acme.modifiers.dto;

import java.io.Serializable;

public class AccountDTO implements Serializable {
    private static final long serialVersionUID = 1L;
    public static final String CONST = "x";
    private transient String cache;
    private Long mpCode;
    private String name;
}
