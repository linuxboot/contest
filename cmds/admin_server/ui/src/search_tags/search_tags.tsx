import React, { useState } from 'react';
import { Input, Checkbox } from 'rsuite';
import TagsGrid from './tags_grid/tags_grid';
import './search_tags.scss';

const ProjectPrefix = 'project_';

export default function SearchTags() {
    const [tagPattern, setTagPattern] = useState<string>('');
    const [searchProjects, setSearchProjects] = useState<boolean>(false);

    const setPrefix = (checked: boolean) => {
        if (checked) {
            if (!tagPattern.startsWith(ProjectPrefix))
                setTagPattern((value) => `${ProjectPrefix}${value}`);
        }
    };

    return (
        <div className="tags-search">
            <div className="tags-search__input-group">
                <p> Search: </p>
                <Input
                    className="filter-input"
                    placeholder="pattern"
                    value={tagPattern}
                    onChange={(value) => {
                        // handles making the project prefix always in the input field in case of project search
                        if (
                            searchProjects &&
                            (value ===
                                ProjectPrefix.substring(
                                    0,
                                    ProjectPrefix.length - 1
                                ) ||
                                value === '')
                        ) {
                            setTagPattern(ProjectPrefix);
                        } else {
                            setTagPattern(value);
                        }
                    }}
                />
                <Checkbox
                    checked={searchProjects}
                    onChange={(_, checked) => {
                        setSearchProjects(checked);
                        setPrefix(checked);
                    }}
                >
                    Search Projects
                </Checkbox>
            </div>
            <TagsGrid tagPattern={tagPattern} />
        </div>
    );
}
