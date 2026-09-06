package com.example.demo;

import java.util.Map;

import com.alipay.common.tracer.core.context.trace.SofaTraceContext;
import com.alipay.common.tracer.core.holder.SofaTraceContextHolder;
import com.alipay.common.tracer.core.span.SofaTracerSpan;
import com.alipay.sofa.rpc.context.RpcInvokeContext;

public class EchoFacadeImpl implements EchoFacade {

    @Override
    public String echo(String message) {
        // Print the trace id the server span actually adopted. This is the
        // single fact the whole change depends on.
        SofaTraceContext traceContext = SofaTraceContextHolder.getSofaTraceContext();
        SofaTracerSpan span = traceContext == null ? null : traceContext.getCurrentSpan();
        String traceId = span == null ? "<no-span>" : span.getSofaTracerSpanContext().getTraceId();
        String rpcId = span == null ? "<no-span>" : span.getSofaTracerSpanContext().getSpanId();

        Map<String, String> baggage = RpcInvokeContext.getContext().getAllRequestBaggage();
        System.out.println("SERVER_TRACE traceId=" + traceId
                + " rpcId=" + rpcId
                + " baggage=" + baggage
                + " message=" + message);
        System.out.flush();
        return "echo:" + message;
    }
}
