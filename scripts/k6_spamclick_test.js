import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { SharedArray } from 'k6/data';

const users = new SharedArray('users', function () {
    return JSON.parse(open('./flashsale_users.json'));
});

export const options = {
    scenarios: {
        spamclick_test: {
            executor: 'shared-iterations',
            vus: 500,          // 500 Concurrent requests
            iterations: 500,   // User clicks 500 times instantly
            maxDuration: '1m',
        },
    },
};

export function setup() {
    // Setup runs once. We find exactly 1 user who successfully booked a seat.
    for (let i = 0; i < users.length; i++) {
        const token = users[i].token;
        const headers = { 'Authorization': 'Bearer ' + token };
        let getRes = http.get('http://localhost:8080/api/v1/me/registrations', { headers });
        if (getRes.status === 200) {
            let regList = JSON.parse(getRes.body);
            if (regList.data && regList.data.length > 0) {
                // Found a user with a seat!
                return { token: token, regID: regList.data[0].id };
            }
        }
    }
    return null; // Should not happen if a test was run before
}

export default function (data) {
    if (!data) {
        console.log("NO USER FOUND WITH SEAT");
        return;
    }

    const headers = {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer ' + data.token
    };

    // STEP: Spam Create Payment Link (User clicks 500 times)
    const createPayUrl = 'http://localhost:8080/api/v1/payments/payos/create';
    const payPayload = JSON.stringify({
        exam_registration_id: data.regID
    });

    let res = http.post(createPayUrl, payPayload, { headers });
    
    check(res, {
        'created successfully (201)': (r) => r.status === 201,
        'blocked duplicate (409)': (r) => r.status === 409,
    });
}
