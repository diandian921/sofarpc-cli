package com.acme.modern.facade;

import com.acme.modern.dto.OrderDTO;
import com.acme.modern.dto.OrderQuery;

public interface OrderFacade {
    /** 按订单号查询 */
    OrderDTO queryOrder(String orderId);

    /** 按条件查询 */
    OrderDTO queryOrder(OrderQuery query);
}
