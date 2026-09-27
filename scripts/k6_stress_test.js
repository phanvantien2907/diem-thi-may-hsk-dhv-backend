import http from 'k6/http';
import { check, sleep } from 'k6';
import crypto from 'k6/crypto';

const checksumKey = "16daddd823fb554d17fc26bb8a769d20ef30620eaa546c1a0398a724cd5be90b";

export const options = {
    scenarios: {
        stress_test: {
            executor: 'ramping-vus',
            startVUs: 0,
            stages: [
                { duration: '5s', target: 50 },
                { duration: '5s', target: 100 },
                { duration: '5s', target: 200 },
                { duration: '10s', target: 500 },
                { duration: '10s', target: 1000 },
            ],
            gracefulRampDown: '5s',
        },
    },
};

export default function () {
    // Tấn công vào API Webhook (ghi nhận liên tục)
    let webhookData = {
        orderCode: Math.floor(Math.random() * 10000000),
        amount: 500000,
        reference: "STRESS_" + __VU,
        accountNumber: "123", transactionDateTime: "2026",
        currency: "VND", paymentLinkId: "LINK", code: "00", desc: "success",
        counterAccountBankId: "B", counterAccountBankName: "B", counterAccountName: "C",
        counterAccountNumber: "1", virtualAccountName: "V", virtualAccountNumber: "1"
    };
    const keys = Object.keys(webhookData).sort();
    let sigParts = [];
    keys.forEach(k => sigParts.push(`${k}=${webhookData[k]}`));
    const signature = crypto.hmac('sha256', checksumKey, sigParts.join('&'), 'hex');

    let payload = JSON.stringify({
        code: "00",
        success: true,
        data: webhookData,
        signature: signature
    });

    // Spam liên tục vào hệ thống
    let res = http.post('http://localhost:8080/api/v1/payments/callback', payload, {
        headers: { 'Content-Type': 'application/json' },
        timeout: '5s' // Cắt request nếu server treo quá 5s
    });

    check(res, {
        'Status is 200': (r) => r.status === 200,
    });
    
    // Test Read API xen kẽ
    let getRes = http.get('http://localhost:8080/api/v1/payments?candidate_id=99999', {
        timeout: '5s'
    });
    
    check(getRes, {
        'GET Status is 200 or 401': (r) => r.status === 200 || r.status === 401,
    });
}
