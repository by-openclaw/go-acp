import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { SingleSourceAssociationNamesRequestMessageCommandParams } from './params';

/**
 * Validator of the SingleSourceAssociationNamesRequestMessage command parameters
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<SingleSourceAssociationNamesRequestMessageCommandParams>}
 */
export class CommandParamsValidator implements IValidator<SingleSourceAssociationNamesRequestMessageCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {SingleSourceAssociationNamesRequestMessageCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: SingleSourceAssociationNamesRequestMessageCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {Record<string, LocaleData>} the localized validation errors
     * @memberof SingleSourceAssociationNamesRequestMessageCommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isMatrixIdOutOfRange()) {
            errors.matrixId = cache.getCommandErrorLocaleData(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isSourceIdOutOfRange()) {
            errors.sourceId = cache.getCommandErrorLocaleData(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        return errors;
    }

    /**
     * Gets a boolean indicating whether the matrixId is out of range
     * + false = matrixId is included within the limits
     * + true = matrixId [0-255] is out of range
     * @private
     * @returns {boolean} indicating if matrixId is out of range
     * @memberof SingleSourceAssociationNamesRequestMessageCommandParamsValidator
     */
    private isMatrixIdOutOfRange(): boolean {
        return this.data.matrixId < 0 || this.data.matrixId > 15;
    }

    /**
     * Gets a boolean indicating whether the sourceId is out of range
     * + false = sourceId is included within the limits
     * + true = sourceId  [0-65535] is out of range
     * @private
     * @returns {boolean}
     * @memberof CrossPointConnectMessageCommandParamsValidator
     */
    private isSourceIdOutOfRange(): boolean {
        return this.data.sourceId < 0 || this.data.sourceId > 65535;
    }
}
