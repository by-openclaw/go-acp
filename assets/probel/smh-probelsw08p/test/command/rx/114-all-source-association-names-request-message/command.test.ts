import { AllSourceAssociationNamesRequestMessageCommand } from '../../../../src/command/rx/114-all-source-association-names-request-message/command';
import { AllSourceAssociationNamesRequestMessageCommandParams } from '../../../../src/command/rx/114-all-source-association-names-request-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { AllSourceAssociationNamesRequestMessageCommandOptions } from '../../../../src/command/rx/114-all-source-association-names-request-message/options';
import { LengthOfNamesRequired } from '../../../../src/command/shared/length-of-names-required';

describe('All Source Association Names Request Message Command)', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: AllSourceAssociationNamesRequestMessageCommandParams = {
                matrixId: 0
            };
            const options: AllSourceAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
            };
            // Act
            const command = new AllSourceAssociationNamesRequestMessageCommand(params, options);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: AllSourceAssociationNamesRequestMessageCommandParams = {
                matrixId: -1
            };

            const options: AllSourceAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: -1
            };
            // Act
            const fct = () => new AllSourceAssociationNamesRequestMessageCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: AllSourceAssociationNamesRequestMessageCommandParams = {
                matrixId: 256
            };

            const options: AllSourceAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: 10
            };
            // Act
            const fct = () => new AllSourceAssociationNamesRequestMessageCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General All Source Names Request Message CMD_114_0X72', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.ALL_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: AllSourceAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0
                };

                const options: AllSourceAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new AllSourceAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '72 00 00', // data
                    3, // bytesCount
                    0x8b, // checksum
                    '10 02 72 00 00 03 8b 10 03' // buffer
                );
            });
            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: AllSourceAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0
                };

                const options: AllSourceAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new AllSourceAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '72 00 01', // data
                    3, // bytesCount
                    0x8a, // checksum
                    '10 02 72 00 01 03 8a 10 03' // buffer
                );
            });
            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: AllSourceAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0
                };

                const options: AllSourceAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new AllSourceAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '72 00 02', // data
                    3, // bytesCount
                    0x89, // checksum
                    '10 02 72 00 02 03 89 10 03' // buffer
                );
            });
            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: AllSourceAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0
                };

                const options: AllSourceAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.SIXTEEN_CHAR_NAMES
                };
                // Act
                const command = new AllSourceAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '72 00 03', // data
                    3, // bytesCount
                    0x88, // checksum
                    '10 02 72 00 03 03 88 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the General and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: AllSourceAssociationNamesRequestMessageCommandParams = {
                matrixId: 0
            };

            const options: AllSourceAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
            };
            // Act
            const command = new AllSourceAssociationNamesRequestMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
