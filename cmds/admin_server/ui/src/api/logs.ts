import superagent from 'superagent';

// TODO: remove the hardcoded levels
// logLevels is the possible levels for logs
export const Levels = ['panic', 'fatal', 'error', 'warning', 'info', 'debug'];

// Log defines the expected log entry returned from the api
export interface Log {
    job_id: number;
    log_data: string;
    log_level: string;
    date: string;
}

// Query defines all the possible filters to form a query
export interface Query {
    job_id?: number;
    text?: string;
    log_level?: string;
    start_date?: string;
    end_date?: string;
    page: number;
    page_size: number;
}

// Result defines the structure of the api response to a query
export interface Result {
    logs: Log[] | null;
    count: number;
    page: number;
    page_size: number;
}

// getLogs returns Result that contains logs fetched according to the Query
export async function getLogs(query: Query): Promise<Result> {
    let result: superagent.Response = await superagent.get('/log').query(query);

    return result.body;
}
