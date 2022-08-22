import superagent from 'superagent';

export interface Tag {
    name: string;
    jobs_count: number;
}

export interface TagQuery {
    text: string;
}

export interface Report {
    name: string;
    time: string;
    success: boolean;
    data: string;
}
export interface Job {
    job_id: number;
    report?: Report;
}

export async function getTags(query: TagQuery): Promise<Tag[]> {
    let result: superagent.Response = await superagent.get('/tag').query(query);

    return result.body;
}

export async function getJobs(job_name: string): Promise<Job[]> {
    let result: superagent.Response = await superagent.get(
        `/tag/${job_name}/jobs`
    );

    return result.body;
}
