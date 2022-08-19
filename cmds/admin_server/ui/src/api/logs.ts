import superagent from 'superagent';

// Result defines the structure of the api response to a query
export interface Result {
    logs: any;
    count: number;
    page: number;
    page_size: number;
}

// getLogs returns Result that contains logs fetched according to the Query
export async function getLogs(query: any): Promise<Result> {
    let result: superagent.Response = await superagent.get('/log').query(query);

    return result.body;
}

export enum FieldType {
    INT = 'int',
    UINT = 'uint',
    STRING = 'string',
    TIME = 'time',
    ENUM = 'enum',
}

export interface FieldMetaData {
    name: string;
    type: string;
    values: string[];
}

export async function getLogDescription(): Promise<FieldMetaData[]> {
    let result: superagent.Response = await superagent.get('/log-description');

    return result.body;
}
