import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams } from './params';

/**
 * CrossPoint Connect On Go Group Salvo Acknowledge Message command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams>}
 */
export class CommandParamsValidator
    implements IValidator<CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {(Record<string, LocaleData>)}
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isMatrixIdOutOfRange()) {
            errors.matrixId = cache.getCommandErrorLocaleData(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isLevelIdOutOfRange()) {
            errors.levelId = cache.getCommandErrorLocaleData(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
        }

        if (this.isDestinationIdOutOfRange()) {
            errors.destinationId = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isSourceIdOutOfRange()) {
            errors.sourceId = cache.getCommandErrorLocaleData(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isSalvoIdOutOfRange()) {
            errors.salvoId = cache.getCommandErrorLocaleData(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        return errors;
    }

    /**
     *  Gets a boolean indicating whether thematrixId is out of range
     * + false = matrixId is included within the limits
     * + true = matrixId [0-255] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isMatrixIdOutOfRange(): boolean {
        return this.data.matrixId < 0 || this.data.matrixId > 255;
    }

    /**
     *  Gets a boolean indicating whether thelevelId is out of range
     * + false = levelId is included within the limits
     * + true = levelId [0-255] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isLevelIdOutOfRange(): boolean {
        return this.data.levelId < 0 || this.data.levelId > 255;
    }
    /**
     *  Gets a boolean indicating whether thedestinationId is out of range
     * + false = destinationId is included within the limits
     * + true = destinationId  [0-65535] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isDestinationIdOutOfRange(): boolean {
        return this.data.destinationId < 0 || this.data.destinationId > 65535;
    }

    /**
     *  Gets a boolean indicating whether thesourceId is out of range
     * + false = sourceId is included within the limits
     * + true = sourceId  [0-65535] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isSourceIdOutOfRange(): boolean {
        return this.data.sourceId < 0 || this.data.sourceId > 65535;
    }

    /**
     *  Gets a boolean indicating whether thesalvoId is out of range
     * + false = salvoId is included within the limits
     * + true = salvoId [0-127] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isSalvoIdOutOfRange(): boolean {
        return this.data.salvoId < 0 || this.data.salvoId > 127;
    }
}
