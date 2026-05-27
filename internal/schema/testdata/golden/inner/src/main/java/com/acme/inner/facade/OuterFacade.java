package com.acme.inner.facade;

import java.util.List;

/**
 * Outer facade with inner DTOs.
 */
public interface OuterFacade {
    /** 列出所有 page */
    List<PageResult> listPages(PageQuery query);

    class PageQuery {
        public Long mpCode;
        public int offset;
    }

    class PageResult {
        public String name;
        public List<String> tags;
    }
}
