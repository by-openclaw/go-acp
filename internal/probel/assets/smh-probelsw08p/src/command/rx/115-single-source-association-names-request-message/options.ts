import { LengthOfNamesRequired } from '../../shared/length-of-names-required';

/**
 * SingleSourceAssociationNamesRequestMessage command options

 * @export
 * @interface SingleSourceAssociationNamesRequestMessageCommandOptions
 */
export interface SingleSourceAssociationNamesRequestMessageCommandOptions {
    /**
     * Length of Names Required
     *
     * @type {LengthOfNamesRequired}
     * @memberof LengthOfNamesRequiredCommandOptions
     */
    lengthOfNames: LengthOfNamesRequired;
}

/**
 * Utility class providing extra functionalities on the SingleSourceAssociationNamesRequestMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @param {SingleSourceAssociationNamesRequestMessageCommandOptions} options the command options
     * @returns {string} the textual representation
     */
    static toString(options: SingleSourceAssociationNamesRequestMessageCommandOptions): string {
        return `lengthOfNames:${this.toStringLengthOfNamesRequired(options.lengthOfNames)}`;
    }

    /**
     * Gets a textual representation of the MaintenanceFunction
     *
     *
     * @param {LengthOfNamesRequired} data the MaintenanceFunction
     * @returns {string} the textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringLengthOfNamesRequired(data: LengthOfNamesRequired): string {
        return `( ${data} - ${LengthOfNamesRequired[data]} )`;
    }
}
