package com.sofarpc.daemon.rpc;

import com.alipay.hessian.generic.model.GenericObject;
import org.junit.Test;

import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.Map;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

/**
 * Verifies conversion from JSON maps to SOFA generic Hessian objects.
 *
 * @author wuwh
 */
public class GenericArgumentConverterTest {

    @Test
    public void wrapsPojoMapArgumentsAsGenericObject() {
        Map<String, Object> request = new LinkedHashMap<String, Object>();
        request.put("mpCode", 433905635109773312L);

        Object[] out = GenericArgumentConverter.convert(
                new String[]{"com.thfund.salesfundmp.facade.model.request.QueryPortfolioAssetRequest"},
                new Object[]{request});

        assertTrue(out[0] instanceof GenericObject);
        GenericObject obj = (GenericObject) out[0];
        assertEquals("com.thfund.salesfundmp.facade.model.request.QueryPortfolioAssetRequest", obj.getType());
        assertEquals(433905635109773312L, obj.getField("mpCode"));
    }

    @Test
    public void leavesJdkMapArgumentsAsMap() {
        Map<String, Object> request = new LinkedHashMap<String, Object>();
        request.put("name", "alice");

        Object[] out = GenericArgumentConverter.convert(new String[]{"java.util.Map"}, new Object[]{request});

        assertTrue(out[0] instanceof Map);
        assertEquals(request, out[0]);
    }

    @Test
    public void supportsExplicitNestedGenericObjectType() {
        Map<String, Object> child = new LinkedHashMap<String, Object>();
        child.put("__type", "com.example.Child");
        child.put("id", 7);
        Map<String, Object> parent = new LinkedHashMap<String, Object>();
        parent.put("child", child);
        parent.put("tags", Arrays.<Object>asList(child));

        Object[] out = GenericArgumentConverter.convert(new String[]{"com.example.Parent"}, new Object[]{parent});

        GenericObject parentObj = (GenericObject) out[0];
        assertTrue(parentObj.getField("child") instanceof GenericObject);
        assertEquals("com.example.Child", ((GenericObject) parentObj.getField("child")).getType());
    }
}
