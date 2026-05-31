package com.acme.inherit.facade;

import com.acme.inherit.dto.OrderDTO;

public interface OrderFacade {
    /** 创建订单 */
    OrderDTO createOrder(OrderDTO order);
}
